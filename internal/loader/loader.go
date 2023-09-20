package loader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
)

var (
	loaderDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "media_proxy_loader_duration_seconds",
		Help:    "Loader duration in seconds",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10},
	}, []string{"status_code"})
	loaderResponseSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "media_proxy_loader_response_size_bytes",
		Help:    "Loader response size in bytes",
		Buckets: []float64{100, 500, 1000, 5000, 10000, 50000, 100000},
	})
)

type Loader interface {
	GetMedia(ctx context.Context, key string) ([]byte, error)
}

type HTTPLoader struct {
	baseURL string
	client  *http.Client
}

func NewHTTPLoader(baseURL string) *HTTPLoader {
	return &HTTPLoader{
		baseURL: baseURL,
		client: &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{},
		},
	}
}

func (l *HTTPLoader) GetMedia(ctx context.Context, mediaPath string) ([]byte, error) {
	upstreamURL, err := url.Parse(fmt.Sprintf("%s%s", l.baseURL, mediaPath))
	if err != nil {
		return nil, fmt.Errorf("failed to parse upstream URL: %w", err)
	}

	startTime := time.Now()
	statusCode := 0
	defer func() {
		loaderDuration.WithLabelValues(fmt.Sprintf("%d", statusCode)).Observe(time.Since(startTime).Seconds())
	}()
	log.Debug().Msgf("Fetching image from %s", upstreamURL.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}
	defer resp.Body.Close()
	statusCode = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			body = []byte(fmt.Sprintf("failed to read response body: %s", resp.Status))
		}
		return nil, fmt.Errorf("failed to fetch image: %s. Body: %q", resp.Status, body)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	loaderResponseSize.Observe(float64(len(bodyBytes)))
	return bodyBytes, nil
}
