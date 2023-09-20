package server

import (
	"net/http"
	"net/url"

	"github.com/blesswinsamuel/media-proxy/internal/cache"
	"github.com/blesswinsamuel/media-proxy/internal/mediaprocessor"
	"github.com/gorilla/schema"
	"github.com/rs/zerolog/log"
)

func parseMetadataQuery(query url.Values) (*mediaprocessor.MetadataOptions, error) {
	metadataOpts := &mediaprocessor.MetadataOptions{}
	var decoder = schema.NewDecoder()
	decoder.SetAliasTag("query")
	if err := decoder.Decode(metadataOpts, query); err != nil {
		return nil, err
	}
	return metadataOpts, nil
}

func (s *server) handleMetadataRequest(w http.ResponseWriter, r *http.Request) {
	info, err := getRequestInfo(s, r, "metadata", parseMetadataQuery)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get request info")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logger := log.With().Str("method", r.Method).Stringer("url", r.URL).Logger()
	ctx := logger.WithContext(r.Context())
	logger.Debug().Interface("opts", info.RequestParams).Msg("Incoming Request")

	params := info.RequestParams
	out, err := cache.GetCachedOrFetch(s.metadataCache, info.MediaPath+"?"+r.URL.Query().Encode(), func() ([]byte, error) {
		imageBytes, err := s.getOriginalImage(ctx, info.MediaPath)
		if err != nil {
			return nil, err
		}
		out, err := s.mediaProcessor.ProcessMetadataRequest(imageBytes, params)
		if err != nil {
			return nil, err
		}
		return out, nil
	})
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		log.Error().Err(err).Msg("Failed to process metadata request")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(out)
}
