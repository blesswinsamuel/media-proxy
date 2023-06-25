package mediaprocessor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"net/http"

	"github.com/bbrks/go-blurhash"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/galdor/go-thumbhash"
)

type ReadOptions struct {
	Dpi  int `query:"dpi"`
	Page int `query:"page"`
}

type MetadataOptions struct {
	Read ReadOptions `query:"read"`
	// https://evanw.github.io/thumbhash/
	ThumbHash bool `query:"thumbhash"`
	// https://github.com/woltapp/blurhash
	BlurHash bool `query:"blurhash"`
}

type TransformOptionsResize struct {
	Width       int    `query:"width"`
	Height      int    `query:"height"`
	Interesting string `query:"interesting"`
	// Method  string // fill or fit
	// Gravity string // valid if method is fill. top, bottom, left, right, center, top right, top left, bottom right, bottom left, smart
}

type TransformOptions struct {
	Raw          bool                    `query:"raw"`
	Read         ReadOptions             `query:"read"`
	Resize       *TransformOptionsResize `query:"resize"`
	OutputFormat string                  `query:"outputFormat"`
}

type MediaProcessor struct {
}

func NewMediaProcessor() *MediaProcessor {
	return &MediaProcessor{}
}

func getContentType(imageBytes []byte) string {
	contentType := http.DetectContentType(imageBytes)
	fmt.Println(contentType)
	if contentType == "text/xml; charset=utf-8" {
		contentType = "image/svg+xml"
	}
	return contentType
}

func parseVipsInteresting(interesting string) (vips.Interesting, error) {
	switch interesting {
	case "none", "":
		return vips.InterestingNone, nil
	case "centre":
		return vips.InterestingCentre, nil
	case "entropy":
		return vips.InterestingEntropy, nil
	case "attention":
		return vips.InterestingAttention, nil
	case "low":
		return vips.InterestingLow, nil
	case "high":
		return vips.InterestingHigh, nil
	case "all":
		return vips.InterestingAll, nil
	case "last":
		return vips.InterestingLast, nil
	default:
		return 0, fmt.Errorf("invalid interesting parameter: %s", interesting)
	}
}

func (mp *MediaProcessor) ProcessTransformRequest(imageBytes []byte, params *TransformOptions) ([]byte, string, error) {
	// Load the image using libvips
	importParams := vips.NewImportParams()
	if params.Read.Dpi > 0 {
		importParams.Density.Set(params.Read.Dpi)
	}
	if params.Read.Page > 0 {
		importParams.Page.Set(params.Read.Page - 1)
	}
	if params.Raw {
		return imageBytes, getContentType(imageBytes), nil
	}

	image, err := vips.LoadImageFromBuffer(imageBytes, importParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load image: %v", err)
	}
	defer image.Close()

	// height := image.Height() * width / image.Width()
	if resize := params.Resize; resize != nil {
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
		interesting, err := parseVipsInteresting(resize.Interesting)
		if err != nil {
			return nil, "", fmt.Errorf("invalid interesting parameter: %s", resize.Interesting)
		}
		err = image.Thumbnail(width, height, interesting)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resize image: %v", err)
		}
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

func (mp *MediaProcessor) ProcessMetadataRequest(imageBytes []byte, params *MetadataOptions) ([]byte, string, error) {
	importParams := vips.NewImportParams()
	if params.Read.Dpi > 0 {
		importParams.Density.Set(params.Read.Dpi)
	}
	if params.Read.Page > 0 {
		importParams.Page.Set(params.Read.Page - 1)
	}

	img, err := vips.LoadImageFromBuffer(imageBytes, importParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load image: %v", err)
	}
	defer img.Close()

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
