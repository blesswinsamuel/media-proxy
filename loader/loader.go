package loader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/blesswinsamuel/media-proxy/cache"
	"github.com/rs/zerolog/log"
)

type Loader interface {
	GetMedia(ctx context.Context, key string) ([]byte, error)
}

type HTTPLoader struct {
	baseURL string
	cache   cache.Cache
}

func NewHTTPLoader(baseURL string, cache cache.Cache) *HTTPLoader {
	return &HTTPLoader{
		baseURL: baseURL,
		cache:   cache,
	}
}

func (l *HTTPLoader) GetMedia(ctx context.Context, mediaPath string) ([]byte, error) {
	upstreamURL, err := url.Parse(fmt.Sprintf("%s%s", l.baseURL, mediaPath))
	if err != nil {
		return nil, fmt.Errorf("failed to parse upstream URL: %w", err)
	}

	cacheKey := cache.Sha256Hash(upstreamURL.String())
	return cache.GetCachedOrFetch(l.cache, cacheKey, func() ([]byte, error) {
		return l.fetchMediaFromUpstream(ctx, upstreamURL)
	})
}

func (l *HTTPLoader) fetchMediaFromUpstream(ctx context.Context, upstreamURL *url.URL) ([]byte, error) {
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
