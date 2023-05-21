package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/davidbyttow/govips/v2/vips"
)

type MetadataRequest struct {
	// https://evanw.github.io/thumbhash/
	ThumbHash bool
	// https://github.com/woltapp/blurhash
	BlurHash bool
}

type TransformRequestResize struct {
	Width  int
	Height int
}

type TransformRequest struct {
	Resize *TransformRequestResize
}

type RequestParams struct {
	MetadataRequest  *MetadataRequest
	TransformRequest *TransformRequest
}

type MediaProcessor struct {
	cache *FsCache
}

func (mp *MediaProcessor) fetchMediaFromUpstream(ctx context.Context, upstreamURL *url.URL) ([]byte, error) {
	log.Printf("Fetching image from %s", upstreamURL.String())

	httpClient := http.DefaultClient
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}
	// defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			body = []byte(fmt.Sprintf("failed to read response body: %s", resp.Status))
		}
		resp.Body.Close()
		return nil, fmt.Errorf("failed to fetch image: %s. Body: %q", resp.Status, body)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return bodyBytes, nil
}

func (mp *MediaProcessor) fetchCachedMedia(ctx context.Context, key string) ([]byte, error) {
	if exists, err := mp.cache.Exists(key); err != nil {
		return nil, fmt.Errorf("failed to check if image exists in cache: %w", err)
	} else if !exists {
		return nil, nil
	}
	return mp.cache.Get(key)
}

func (mp *MediaProcessor) fetchMedia(ctx context.Context, upstreamURL *url.URL) ([]byte, error) {
	cacheKey := sha256Hash(upstreamURL.String())
	if cachedImage, err := mp.fetchCachedMedia(ctx, cacheKey); err != nil {
		return nil, fmt.Errorf("failed to fetch cached image: %w", err)
	} else if cachedImage != nil {
		log.Printf("Image %s exists in cache %s", upstreamURL.String(), cacheKey)
		return cachedImage, nil
	}
	img, err := mp.fetchMediaFromUpstream(ctx, upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image from upstream: %w", err)
	}
	if err := mp.cache.Put(cacheKey, img); err != nil {
		return nil, fmt.Errorf("failed to cache image: %w", err)
	}
	return img, nil
}

func (mp *MediaProcessor) processRequest(imageBytes []byte, params RequestParams) ([]byte, string, error) {
	if params.MetadataRequest != nil {
		return mp.processMetadataRequest(imageBytes, params.MetadataRequest)
	}
	if params.TransformRequest != nil {
		return mp.processTransformRequest(imageBytes, params.TransformRequest)
	}
	return nil, "", errors.New("bug: no request params specified")
}

func (mp *MediaProcessor) processMetadataRequest(imageBytes []byte, params *MetadataRequest) ([]byte, string, error) {
	// Load the image using libvips
	image, err := vips.NewImageFromBuffer(imageBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load image: %v", err)
	}
	defer image.Close()
	type MetadataResponse struct {
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		NoOfPages int    `json:"noOfPages"`
		Format    string `json:"format"`
	}
	metadata := MetadataResponse{
		Width:     image.Width(),
		Height:    image.Height(),
		NoOfPages: image.Pages(),
		Format:    vips.ImageTypes[image.Format()],
	}
	res, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal metadata: %v", err)
	}
	return res, "application/json", nil
}

func (mp *MediaProcessor) processTransformRequest(imageBytes []byte, params *TransformRequest) ([]byte, string, error) {
	// Load the image using libvips
	image, err := vips.NewImageFromBuffer(imageBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load image: %v", err)
	}
	defer image.Close()

	// height := image.Height() * width / image.Width()
	if params.Resize != nil {
		width := params.Resize.Width
		height := params.Resize.Height
		err = image.Thumbnail(width, height, vips.InterestingAttention)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resize image: %v", err)
		}
	}

	ep := vips.NewDefaultJPEGExportParams()
	outputBytes, _, err := image.Export(ep)
	contentType := "image/jpeg"
	return outputBytes, contentType, err
}

func sha256Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs)
}
