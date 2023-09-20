package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/blesswinsamuel/media-proxy/internal/cache"
	"github.com/blesswinsamuel/media-proxy/internal/loader"
	"github.com/blesswinsamuel/media-proxy/internal/mediaprocessor"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

var (
	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "media_proxy_request_duration_seconds",
		Help:    "Request duration in seconds",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10},
	}, []string{"method", "path", "status_code"})
	activeRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "media_proxy_active_requests",
		Help: "Active requests",
	})
	activeConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "media_proxy_active_conns",
		Help: "Active connections",
	})
	networkConns = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "media_proxy_network_conns_count",
		Help: "Network connections count",
	}, []string{"state"})
)

type ServerConfig struct {
	Port         string
	MetricsPort  string
	Secret       string
	EnableUnsafe bool
	AutoAvif     bool
	AutoWebp     bool
	Concurrency  int
}

type server struct {
	mediaProcessor     *mediaprocessor.MediaProcessor
	loader             loader.Loader
	config             ServerConfig
	srv                *http.Server
	maxConnectionCount int
	loaderCache        cache.Cache
	metadataCache      cache.Cache
	resultCache        cache.Cache
}

func NewServer(config ServerConfig, mediaProcessor *mediaprocessor.MediaProcessor, loader loader.Loader, loaderCache cache.Cache, metadataCache cache.Cache, resultCache cache.Cache) *server {
	mux := chi.NewRouter()
	srv := &http.Server{
		Addr:              ":" + config.Port,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       120 * time.Second,
		ConnState: func(c net.Conn, cs http.ConnState) {
			networkConns.WithLabelValues(cs.String()).Inc()
			switch cs {
			case http.StateNew:
				activeConns.Inc()
			case http.StateHijacked, http.StateClosed:
				activeConns.Dec()
			}
		},
	}
	s := &server{
		config:             config,
		mediaProcessor:     mediaProcessor,
		loader:             loader,
		srv:                srv,
		maxConnectionCount: config.Concurrency,
		loaderCache:        loaderCache,
		metadataCache:      metadataCache,
		resultCache:        resultCache,
	}
	mux.Use(middleware.ThrottleWithOpts(middleware.ThrottleOpts{
		Limit:          s.maxConnectionCount,
		BacklogLimit:   200,
		BacklogTimeout: 60 * time.Second,
	}))
	mux.Use(middleware.RequestID)
	mux.Use(s.prometheusMiddleware)
	mux.HandleFunc("/{signature}/metadata/*", s.handleMetadataRequest)
	mux.HandleFunc("/{signature}/media/*", s.handleTransformRequest)
	return s
}

type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *server) prometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeRequests.Inc()
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(sw, r)
		routePattern := chi.RouteContext(r.Context()).RoutePattern()
		requestDuration.WithLabelValues(r.Method, routePattern, strconv.Itoa(sw.statusCode)).Observe(time.Since(start).Seconds())
		activeRequests.Dec()
	})
}

func (s *server) Start() {
	go func() {
		log.Printf("Server listening on port %s", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("")
		}
	}()
	go func() {
		http.ListenAndServe(":"+s.config.MetricsPort, promhttp.Handler())
	}()
}

func (s *server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}

type HTTPError struct {
	Code           int
	Message        string
	OrigError      error
	AdditionalInfo any
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%d: %s: %s", e.Code, e.Message, e.OrigError)
}

func NewHTTPError(code int, message string, err error) *HTTPError {
	return &HTTPError{
		Code:      code,
		Message:   message,
		OrigError: err,
	}
}

type RequestInfo[T any] struct {
	Signature        string
	MediaPath        string
	RequestParamsRaw url.Values
	RequestParams    *T
}

func getRequestInfo[T any](s *server, r *http.Request, requestType string, parseQuery func(query url.Values) (*T, error)) (*RequestInfo[T], error) {
	// validate signature
	signature := chi.URLParam(r, "signature")
	mediaPath := chi.URLParam(r, "*")

	if !s.config.EnableUnsafe {
		mp := requestType + "/" + mediaPath
		if r.URL.RawQuery != "" {
			mp = mp + "?" + r.URL.RawQuery
		}
		if !s.validateSignature(signature, mp) {
			return nil, NewHTTPError(http.StatusForbidden, "Invalid signature", nil)
		}
	}

	mediaPath = strings.TrimSuffix(mediaPath, "/")

	// parse query
	requestParams, err := parseQuery(r.URL.Query())
	if err != nil {
		return nil, NewHTTPError(http.StatusBadRequest, "Failed to parse query", err)
	}

	return &RequestInfo[T]{
		Signature:        signature,
		MediaPath:        mediaPath,
		RequestParams:    requestParams,
		RequestParamsRaw: r.URL.Query(),
	}, nil
}

func (s *server) getOriginalImage(ctx context.Context, mediaPath string) ([]byte, error) {
	// Perform the request to the target server
	imageBytes, err := cache.GetCachedOrFetch(s.loaderCache, mediaPath, func() ([]byte, error) {
		return s.loader.GetMedia(ctx, mediaPath)
	})
	if err != nil {
		return nil, NewHTTPError(http.StatusInternalServerError, "Failed to fetch image", err)
	}
	return imageBytes, nil
}

func concatenateContentTypeAndData(contentType string, data []byte) []byte {
	sizeBytes := make([]byte, 4, 4+len(contentType)+len(data))
	binary.LittleEndian.PutUint32(sizeBytes, uint32(len(contentType)))
	concatenatedBytes := append(sizeBytes, contentType...)
	concatenatedBytes = append(concatenatedBytes, data...)
	return concatenatedBytes
}

func getContentTypeAndData(concatenatedBytes []byte) (string, []byte) {
	size := binary.LittleEndian.Uint32(concatenatedBytes[:4])
	contentType := string(concatenatedBytes[4 : 4+size])
	data := concatenatedBytes[4+size:]
	return contentType, data
}

func (s *server) validateSignature(signature string, imagePath string) bool {
	hash := hmac.New(sha1.New, []byte(s.config.Secret))
	hash.Write([]byte(imagePath))
	expectedHash := base64.URLEncoding.EncodeToString(hash.Sum(nil))
	// expectedHash = expectedHash[:40]
	// log.Debug().Msgf("expected hash (%s): %s", imagePath, expectedHash)
	return expectedHash == signature
}
