package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/blesswinsamuel/media-proxy/cache"
	"github.com/blesswinsamuel/media-proxy/loader"
	"github.com/blesswinsamuel/media-proxy/mediaprocessor"
	"github.com/gorilla/schema"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/netutil"
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
}

type server struct {
	mediaProcessor     *mediaprocessor.MediaProcessor
	loader             loader.Loader
	config             ServerConfig
	srv                *http.Server
	maxConnectionCount int
	metadataCache      cache.Cache
}

func NewServer(config ServerConfig, mediaProcessor *mediaprocessor.MediaProcessor, loader loader.Loader, metadataCache cache.Cache) *server {
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
				activeConns.Add(1)
			case http.StateHijacked, http.StateClosed:
				activeConns.Add(-1)
			}
		},
	}
	s := &server{
		config:             config,
		mediaProcessor:     mediaProcessor,
		loader:             loader,
		srv:                srv,
		maxConnectionCount: 8,
		metadataCache:      metadataCache,
	}
	mux.Use(s.prometheusMiddleware)
	mux.HandleFunc("/{signature}/metadata/*", s.handleMetadataRequest)
	mux.HandleFunc("/{signature}/media/*", s.handleMediaRequest)
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
		l, err := net.Listen("tcp", s.srv.Addr)
		if err != nil {
			log.Fatal().Err(err).Msg("")
		}
		defer l.Close()
		l = netutil.LimitListener(l, s.maxConnectionCount)

		log.Printf("Server listening on port %s", s.srv.Addr)
		if err := s.srv.Serve(l); err != nil && err != http.ErrServerClosed {
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
	Signature     string
	MediaPath     string
	RequestParams *T
	ImageBytes    []byte
}

func getRequestInfo[T any](s *server, r *http.Request, requestType string, parseQuery func(query url.Values) (*T, error)) (*RequestInfo[T], error) {
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

	requestParams, err := parseQuery(r.URL.Query())
	if err != nil {
		return nil, NewHTTPError(http.StatusBadRequest, "Failed to parse query", err)
	}
	log.Debug().Str("method", r.Method).Stringer("url", r.URL).Any("opts", requestParams).Msg("Incoming Request")

	// Perform the request to the target server
	imageBytes, err := s.loader.GetMedia(r.Context(), mediaPath)
	if err != nil {
		return nil, NewHTTPError(http.StatusInternalServerError, "Failed to fetch image", err)
	}

	return &RequestInfo[T]{
		Signature:     signature,
		MediaPath:     mediaPath,
		ImageBytes:    imageBytes,
		RequestParams: requestParams,
	}, nil
}

func (s *server) handleMetadataRequest(w http.ResponseWriter, r *http.Request) {
	info, err := getRequestInfo(s, r, "metadata", parseMetadataQuery)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get request info")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	params := info.RequestParams
	cacheKey := cache.Sha256Hash(info.Signature + info.MediaPath)
	out, err := cache.GetCachedOrFetch(s.metadataCache, cacheKey, func() ([]byte, error) {
		out, contentType, err := s.mediaProcessor.ProcessMetadataRequest(info.ImageBytes, params)
		if err != nil {
			return nil, err
		}
		w.Header().Set("Content-Type", contentType)
		return out, nil
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to process metadata request")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(out)
}

func (s *server) handleMediaRequest(w http.ResponseWriter, r *http.Request) {
	info, err := getRequestInfo(s, r, "media", parseTransformQuery)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get request info")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	params := info.RequestParams
	if params.OutputFormat == "" {
		contentType := http.DetectContentType(info.ImageBytes)
		acceptedContentTypes := strings.Split(r.Header.Get("Accept"), ",")
		if len(acceptedContentTypes) > 0 {
			for _, acceptedContentType := range acceptedContentTypes {
				if acceptedContentType == "image/avif" {
					continue
				}
				if strings.HasPrefix(acceptedContentType, "image/") {
					contentType = strings.TrimSpace(acceptedContentType)
					break
				}
			}
		}
		switch contentType {
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
		default:
			params.OutputFormat = "png"
		}
	}

	out, contentType, err := s.mediaProcessor.ProcessTransformRequest(info.ImageBytes, params)
	if err != nil {
		log.Error().Err(err).Msg("Failed to process metadata request")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.Write(out)
}

func (s *server) validateSignature(signature string, imagePath string) bool {
	hash := hmac.New(sha1.New, []byte(s.config.Secret))
	hash.Write([]byte(imagePath))
	expectedHash := base64.URLEncoding.EncodeToString(hash.Sum(nil))
	// expectedHash = expectedHash[:40]
	// log.Debug().Msgf("expected hash (%s): %s", imagePath, expectedHash)
	return expectedHash == signature
}

func parseMetadataQuery(query url.Values) (*mediaprocessor.MetadataOptions, error) {
	metadataOpts := &mediaprocessor.MetadataOptions{}
	var decoder = schema.NewDecoder()
	decoder.SetAliasTag("query")
	if err := decoder.Decode(metadataOpts, query); err != nil {
		return nil, err
	}
	return metadataOpts, nil
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
