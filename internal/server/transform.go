package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/blesswinsamuel/media-proxy/internal/cache"
	"github.com/blesswinsamuel/media-proxy/internal/mediaprocessor"
	"github.com/gorilla/schema"
	"github.com/rs/zerolog/log"
)

func (s *server) handleTransformRequest(w http.ResponseWriter, r *http.Request) {
	info, err := getRequestInfo(s, r, "media", parseTransformQuery)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get request info")
		if httpErr, ok := err.(*HTTPError); ok {
			http.Error(w, httpErr.Message, httpErr.Code)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	logger := log.With().Str("method", r.Method).Stringer("url", r.URL).Logger()
	ctx := logger.WithContext(r.Context())
	logger.Debug().Interface("opts", info.RequestParams).Msg("Incoming Request")

	params := info.RequestParams

	if params.OutputFormat == "" {
		selectedContentType := ""
		acceptedContentTypes := strings.Split(r.Header.Get("Accept"), ",")
		if len(acceptedContentTypes) > 0 {
			for _, acceptedContentType := range acceptedContentTypes {
				if acceptedContentType == "image/avif" {
					// TODO: find why I disabled avif
					continue
				}
				if strings.HasPrefix(acceptedContentType, "image/") {
					selectedContentType = strings.TrimSpace(acceptedContentType)
					break
				}
			}
		}
		switch selectedContentType {
		case "image/webp":
			params.OutputFormat = "webp"
		case "image/jpeg":
			params.OutputFormat = "jpeg"
		case "image/png":
			params.OutputFormat = "png"
		case "image/avif":
			params.OutputFormat = "avif"
		case "image/apng":
			params.OutputFormat = "apng"
		}
	}

	out, err := cache.GetCachedOrFetch(s.resultCache, info.MediaPath+"?"+params.String(), func() ([]byte, error) {
		imageBytes, err := s.getOriginalImage(ctx, info.MediaPath)
		if err != nil {
			return nil, err
		}
		if params.OutputFormat == "" {
			params.OutputFormat = "png"
		}

		out, contentType, err := s.mediaProcessor.ProcessTransformRequest(imageBytes, params)
		if err != nil {
			return nil, err
		}
		return concatenateContentTypeAndData(contentType, out), nil
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to process transform request")
		if httpErr, ok := err.(*HTTPError); ok {
			http.Error(w, httpErr.Message, httpErr.Code)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	contentType, out := getContentTypeAndData(out)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.Write(out)
}

func parseTransformQuery(query url.Values) (*mediaprocessor.TransformOptions, error) {
	transformOpts := &mediaprocessor.TransformOptions{}
	var decoder = schema.NewDecoder()
	decoder.SetAliasTag("query")
	if err := decoder.Decode(transformOpts, query); err != nil {
		return nil, err
	}
	return transformOpts, nil
}
