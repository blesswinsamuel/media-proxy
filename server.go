package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func main() {
	// Set up libvips concurrency level
	vips.Startup(&vips.Config{
		ConcurrencyLevel: 1,
		MaxCacheFiles:    0,
		MaxCacheMem:      50 * 1024 * 1024,
		MaxCacheSize:     100,
		// ReportLeaks      :
		// CacheTrace       :
		// CollectStats     :
	})
	defer vips.Shutdown()

	baseURL := os.Getenv("BASE_URL")
	baseURL = strings.TrimSuffix(baseURL, "/")
	baseURLParsed, err := url.Parse(baseURL)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to parse BASE_URL")
	}
	cachePath := getEnv("CACHE_PATH", "/tmp/cache")
	enableUnsafe, err := strconv.ParseBool(getEnv("ENABLE_UNSAFE", "false"))
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to parse ENABLE_UNSAFE")
	}
	port := getEnv("PORT", "8080")

	cache := &FsCache{
		cachePath: cachePath,
	}

	mediaProcessor := &MediaProcessor{
		cache: cache,
	}

	server := NewServer(ServerConfig{
		Port:         port,
		BaseURL:      baseURLParsed,
		EnableUnsafe: enableUnsafe,
		AutoAvif:     true,
		AutoWebp:     true,
	}, mediaProcessor)

	// Start the server
	server.Start()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info().Msg("Shutting down...")
	server.Stop()
}

type ServerConfig struct {
	Port         string
	BaseURL      *url.URL
	EnableUnsafe bool
	AutoAvif     bool
	AutoWebp     bool
}

type server struct {
	mediaProcessor *MediaProcessor
	config         ServerConfig
	srv            *http.Server
}

func NewServer(config ServerConfig, mediaProcessor *MediaProcessor) *server {
	mux := chi.NewRouter()
	srv := &http.Server{
		Addr:    ":" + config.Port,
		Handler: mux,
	}
	s := &server{
		config:         config,
		mediaProcessor: mediaProcessor,
		srv:            srv,
	}
	mux.HandleFunc("/{signature}/*", s.handleRequest)
	return s
}

func (s *server) Start() {
	go func() {
		log.Printf("Server listening on port %s", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("")
		}
	}()
}

func (s *server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}

func (s *server) handleRequest(w http.ResponseWriter, r *http.Request) {
	signature := chi.URLParam(r, "signature")
	mediaPath := chi.URLParam(r, "*")

	if !s.config.EnableUnsafe {
		if !validateSignature(signature, mediaPath) {
			http.Error(w, "Invalid signature", http.StatusForbidden)
			return
		}
	}

	upstreamURL, err := url.Parse(fmt.Sprintf("%s/%s", s.config.BaseURL.String(), mediaPath))
	if err != nil {
		http.Error(w, "Failed to parse upstream URL", http.StatusInternalServerError)
		log.Error().Err(err).Msg("Failed to parse upstream URL")
		return
	}

	// Perform the request to the target server
	imageBytes, err := s.mediaProcessor.fetchMedia(r.Context(), upstreamURL)
	if err != nil {
		http.Error(w, "Failed to fetch image", http.StatusInternalServerError)
		log.Error().Err(err).Msg("Failed to fetch image")
		return
	}

	requestParams, err := parseQuery(r.URL.Query())
	if err != nil {
		http.Error(w, "Failed to parse query", http.StatusBadRequest)
		log.Error().Err(err).Msg("Failed to parse query")
		return
	}

	// contentType := resp.Header.Get("Content-Type")
	// fmt.Println(contentType)
	// if strings.HasPrefix(contentType, "image/") {
	// Transform the image using libvips
	bytes, contentType, err := s.mediaProcessor.processRequest(imageBytes, *requestParams)
	if err != nil {
		http.Error(w, "Image transformation failed", http.StatusInternalServerError)
		log.Error().Err(err).Msg("Image transformation failed")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(bytes)
	// }
	// http.Error(w, "invalid content type", http.StatusInternalServerError)
}

func validateSignature(signature string, imagePath string) bool {
	return true
}

func parseQuery(query url.Values) (*RequestParams, error) {
	requestOptions := &RequestParams{}

	// metadata request
	metadata, err := strconv.ParseBool(query.Get("metadata"))
	if err != nil {
		return nil, fmt.Errorf("invalid metadata parameter: %s", query.Get("metadata"))
	}
	if metadata {
		requestOptions.MetadataRequest = &MetadataRequest{}
		return requestOptions, nil
	}

	// transform request
	requestOptions.TransformRequest = &TransformRequest{}
	transformRequest := requestOptions.TransformRequest
	resizeParam := query.Get("resize")
	if resizeParam != "" {
		var width, height int
		_, err := fmt.Sscanf(resizeParam, "%dx%d", &width, &height)
		if err != nil {
			return nil, fmt.Errorf("invalid resize parameter: %s", resizeParam)
		}
		transformRequest.Resize = &TransformRequestResize{Width: width, Height: height}
	}

	return requestOptions, nil
}
