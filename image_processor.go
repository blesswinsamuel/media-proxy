package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/davidbyttow/govips/v2/vips"
)

type TransformOptionsResize struct {
	Width  int
	Height int
}

type TransformOptions struct {
	// https://evanw.github.io/thumbhash/
	ThumbHash bool
	// https://github.com/woltapp/blurhash
	BlurHash bool
	Resize   *TransformOptionsResize
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

func (mp *MediaProcessor) processMedia(imageBytes []byte, transformOptions TransformOptions) ([]byte, string, error) {
	// Load the image using libvips
	image, err := vips.NewImageFromBuffer(imageBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load image: %v", err)
	}
	defer image.Close()

	// height := image.Height() * width / image.Width()
	if transformOptions.Resize != nil {
		width := transformOptions.Resize.Width
		height := transformOptions.Resize.Height
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
