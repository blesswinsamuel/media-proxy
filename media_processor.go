package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"

	"github.com/bbrks/go-blurhash"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/galdor/go-thumbhash"
	"github.com/rs/zerolog/log"
)

type MetadataOptions struct {
	// https://evanw.github.io/thumbhash/
	ThumbHash bool
	// https://github.com/woltapp/blurhash
	BlurHash bool
}

type TransformOptionsResize struct {
	Width       int
	Height      int
	Interesting vips.Interesting
	// Method  string // fill or fit
	// Gravity string // valid if method is fill. top, bottom, left, right, center, top right, top left, bottom right, bottom left, smart
}

type TransformOptions struct {
	Resize *TransformOptionsResize
	Dpi    int
	PageNo int
	Raw    bool
}

type RequestParams struct {
	MetadataOptions  *MetadataOptions
	TransformOptions TransformOptions
	OutputFormat     string
}

type MediaProcessor struct {
	cache *FsCache
}

func (mp *MediaProcessor) fetchMediaFromUpstream(ctx context.Context, upstreamURL *url.URL) ([]byte, error) {
	log.Debug().Msgf("Fetching image from %s", upstreamURL.String())

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
		log.Debug().Msgf("Image %s exists in cache %s", upstreamURL.String(), cacheKey)
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

func getContentType(imageBytes []byte) string {
	contentType := http.DetectContentType(imageBytes)
	fmt.Println(contentType)
	if contentType == "text/xml; charset=utf-8" {
		contentType = "image/svg+xml"
	}
	return contentType
}

func (mp *MediaProcessor) processRequest(imageBytes []byte, params RequestParams) ([]byte, string, error) {
	// Load the image using libvips
	importParams := vips.NewImportParams()
	if params.TransformOptions.Dpi > 0 {
		importParams.Density.Set(params.TransformOptions.Dpi)
	}
	if params.TransformOptions.PageNo > 0 {
		importParams.Page.Set(params.TransformOptions.PageNo - 1)
	}
	if params.TransformOptions.Raw {
		return imageBytes, getContentType(imageBytes), nil
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
		if width == 0 {
			width = height * image.Width() / image.Height()
		}
		if height == 0 {
			height = width * image.Height() / image.Width()
		}
		// switch resize.Method {
		// case "fill":
		// 	err = image.Thumbnail(width, height, vips.InterestingAttention)
		// 	if err != nil {
		// 		return nil, "", fmt.Errorf("failed to resize image: %v", err)
		// 	}
		// case "fit":
		// default:
		// 	return nil, "", fmt.Errorf("invalid resize method: %s", resize.Method)
		// }
		err = image.Thumbnail(width, height, resize.Interesting)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resize image: %v", err)
		}
	}

	if params.MetadataOptions != nil {
		return mp.processMetadataRequest(image, params.MetadataOptions)
	}

	switch params.OutputFormat {
	case "jpeg":
		ep := vips.NewDefaultJPEGExportParams()
		outputBytes, _, err := image.Export(ep)
		return outputBytes, "image/jpeg", err
	case "png":
		ep := vips.NewDefaultPNGExportParams()
		outputBytes, _, err := image.Export(ep)
		return outputBytes, "image/png", err
	case "avif":
		ep := vips.NewAvifExportParams()
		outputBytes, _, err := image.ExportAvif(ep)
		return outputBytes, "image/avif", err
	case "webp":
		ep := vips.NewWebpExportParams()
		outputBytes, _, err := image.ExportWebp(ep)
		return outputBytes, "image/webp", err
	default:
		return nil, "", fmt.Errorf("invalid output format: %s", params.OutputFormat)
	}
}

func (mp *MediaProcessor) processMetadataRequest(img *vips.ImageRef, params *MetadataOptions) ([]byte, string, error) {
	type MetadataResponse struct {
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		NoOfPages int    `json:"noOfPages"`
		Format    string `json:"format"`
		Blurhash  string `json:"blurhash,omitempty"`
		Thumbhash string `json:"thumbhash,omitempty"`
	}
	metadata := MetadataResponse{
		Width:     img.Width(),
		Height:    img.Height(),
		NoOfPages: img.Pages(),
		Format:    vips.ImageTypes[img.Format()],
	}
	if params.BlurHash {
		ep := vips.NewDefaultJPEGExportParams()
		ep.Quality = 10
		outputBytes, _, err := img.Export(ep)
		if err != nil {
			return nil, "", fmt.Errorf("failed to export image: %v", err)
		}
		gimg, _, err := image.Decode(bytes.NewReader(outputBytes))
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode image: %v", err)
		}
		hash, err := blurhash.Encode(5, 5, gimg)
		if err != nil {
			return nil, "", fmt.Errorf("failed to encode blurhash: %v", err)
		}
		metadata.Blurhash = hash
	}
	if params.ThumbHash {
		// ep := vips.NewJpegExportParams()
		// ep.Quality = 10
		// outputBytes, _, err := img.ExportJpeg(ep)
		ep := vips.NewPngExportParams()
		ep.Quality = 10
		outputBytes, _, err := img.ExportPng(ep)
		if err != nil {
			return nil, "", fmt.Errorf("failed to export image: %v", err)
		}
		gimg, _, err := image.Decode(bytes.NewReader(outputBytes))
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode image: %v", err)
		}
		metadata.Thumbhash = base64.StdEncoding.EncodeToString(thumbhash.EncodeImage(gimg))
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
