package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/davidbyttow/govips/v2/vips"
)

type MetadataOptions struct {
	// https://evanw.github.io/thumbhash/
	ThumbHash bool
	// https://github.com/woltapp/blurhash
	BlurHash bool
}

type TransformOptionsResize struct {
	Width  int
	Height int
}

type TransformOptions struct {
	Resize *TransformOptionsResize
	Dpi    int
	PageNo int
}

type RequestParams struct {
	MetadataOptions  *MetadataOptions
	TransformOptions TransformOptions
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
	// Load the image using libvips
	importParams := vips.NewImportParams()
	if params.TransformOptions.Dpi > 0 {
		importParams.Density.Set(params.TransformOptions.Dpi)
	}
	if params.TransformOptions.PageNo > 0 {
		importParams.Page.Set(params.TransformOptions.PageNo)
	}

	image, err := vips.LoadImageFromBuffer(imageBytes, importParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load image: %v", err)
	}
	defer image.Close()

	// height := image.Height() * width / image.Width()
	if resize := params.TransformOptions.Resize; resize != nil {
		width := resize.Width
		height := resize.Height
		err = image.Thumbnail(width, height, vips.InterestingAttention)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resize image: %v", err)
		}
	}

	if params.MetadataOptions != nil {
		return mp.processMetadataRequest(image, params.MetadataOptions)
	}

	ep := vips.NewDefaultJPEGExportParams()
	outputBytes, _, err := image.Export(ep)
	contentType := "image/jpeg"
	return outputBytes, contentType, err
}

func (mp *MediaProcessor) processMetadataRequest(image *vips.ImageRef, params *MetadataOptions) ([]byte, string, error) {
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

func sha256Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs)
}
