package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
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
	vips.LoggingSettings(func(messageDomain string, messageLevel vips.LogLevel, message string) {
		var messageLevelDescription string
		switch messageLevel {
		case vips.LogLevelError:
			messageLevelDescription = "error"
		case vips.LogLevelCritical:
			messageLevelDescription = "critical"
		case vips.LogLevelWarning:
			messageLevelDescription = "warning"
		case vips.LogLevelMessage:
			messageLevelDescription = "message"
		case vips.LogLevelInfo:
			messageLevelDescription = "info"
		case vips.LogLevelDebug:
			messageLevelDescription = "debug"
		}
		log.Debug().Str("domain", messageDomain).Str("level", messageLevelDescription).Msg(message)
	}, vips.LogLevelWarning)
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
	if baseURL != "" {
		baseURL = baseURL + "/"
	}
	cachePath := getEnv("CACHE_PATH", "/tmp/cache")
	enableUnsafe, err := strconv.ParseBool(getEnv("ENABLE_UNSAFE", "false"))
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to parse ENABLE_UNSAFE")
	}
	port := getEnv("PORT", "8080")
	secret := getEnv("SECRET", "")
	if !enableUnsafe {
		if secret == "" {
			log.Fatal().Msg("SECRET must be set when ENABLE_UNSAFE=false")
		}
	}

	cache := &FsCache{
		cachePath: cachePath,
	}

	mediaProcessor := &MediaProcessor{
		cache: cache,
	}

	server := NewServer(ServerConfig{
		Port:         port,
		BaseURL:      baseURL,
		Secret:       secret,
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
	BaseURL      string
	Secret       string
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
		mp := mediaPath
		if r.URL.RawQuery != "" {
			mp = mp + "?" + r.URL.RawQuery
		}
		if !s.validateSignature(signature, mp) {
			http.Error(w, "Invalid signature", http.StatusForbidden)
			return
		}
	}

	upstreamURL, err := url.Parse(fmt.Sprintf("%s%s", s.config.BaseURL, mediaPath))
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
		log.Error().Err(err).Msg("Failed to parse query")
		return
	}

	if requestParams.OutputFormat == "" {
		contentType := http.DetectContentType(imageBytes)
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
		case "image/jpeg":
			requestParams.OutputFormat = "jpeg"
		case "image/png":
			requestParams.OutputFormat = "png"
		case "image/avif":
			requestParams.OutputFormat = "avif"
		case "image/webp":
			requestParams.OutputFormat = "webp"
		case "image/apng":
			requestParams.OutputFormat = "apng"
		default:
			requestParams.OutputFormat = "png"
		}
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
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Write(bytes)
	// }
	// http.Error(w, "invalid content type", http.StatusInternalServerError)
}

func (s *server) validateSignature(signature string, imagePath string) bool {
	hash := hmac.New(sha1.New, []byte(s.config.Secret))
	hash.Write([]byte(imagePath))
	expectedHash := base64.URLEncoding.EncodeToString(hash.Sum(nil))
	// expectedHash = expectedHash[:40]
	// log.Debug().Msgf("expected hash (%s): %s", imagePath, expectedHash)
	return expectedHash == signature
}

func parseQuery(query url.Values) (*RequestParams, error) {
	requestOptions := &RequestParams{}

	// metadata request
	if metadata := query.Get("metadata"); metadata != "" {
		metadata, err := strconv.ParseBool(metadata)
		if err != nil {
			return nil, fmt.Errorf("invalid metadata parameter: %q", query.Get("metadata"))
		}
		metadataOptions := &MetadataOptions{}
		if thumbhash := query.Get("metadata.thumbhash"); thumbhash != "" {
			thumbhash, err := strconv.ParseBool(thumbhash)
			if err != nil {
				return nil, fmt.Errorf("invalid thumbhash parameter: %q", query.Get("thumbhash"))
			}
			metadataOptions.ThumbHash = thumbhash
		}
		if blurhash := query.Get("metadata.blurhash"); blurhash != "" {
			blurhash, err := strconv.ParseBool(blurhash)
			if err != nil {
				return nil, fmt.Errorf("invalid blurhash parameter: %q", query.Get("blurhash"))
			}
			metadataOptions.BlurHash = blurhash
		}
		if metadata {
			requestOptions.MetadataOptions = metadataOptions
		}
	}

	// transform request
	transformRequest := TransformOptions{}
	var resizeOpts TransformOptionsResize
	if width := query.Get("resize.width"); width != "" {
		widthInt, err := strconv.Atoi(width)
		if err != nil {
			return nil, fmt.Errorf("invalid resize.width parameter: %s", width)
		}
		resizeOpts.Width = widthInt
	}
	if height := query.Get("resize.height"); height != "" {
		heightInt, err := strconv.Atoi(height)
		if err != nil {
			return nil, fmt.Errorf("invalid resize.height parameter: %s", height)
		}
		resizeOpts.Height = heightInt
	}
	// resizeMethod := query.Get("resize.method") // fill or fit
	// if resizeMethod != "" {
	// 	resizeOpts.Method = resizeMethod
	// } else {
	// 	resizeOpts.Method = "fill"
	// }
	// resizeGravity := query.Get("resize.gravity") // valid if method is fill. top, bottom, left, right, center, top right, top left, bottom right, bottom left, smart
	// if resizeGravity != "" {
	// 	resizeOpts.Gravity = resizeGravity
	// } else {
	// 	resizeOpts.Gravity = "smart"
	// }
	if resizeOpts.Width != 0 || resizeOpts.Height != 0 {
		transformRequest.Resize = &resizeOpts
	}
	dpi := query.Get("dpi")
	if dpi != "" {
		dpiInt, err := strconv.Atoi(dpi)
		if err != nil {
			return nil, fmt.Errorf("invalid dpi parameter: %s", dpi)
		}
		transformRequest.Dpi = dpiInt
	}
	raw := query.Get("raw")
	if raw != "" {
		rawBool, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid raw parameter: %s", dpi)
		}
		transformRequest.Raw = rawBool
	}

	pageNo := query.Get("page")
	if pageNo != "" {
		pageNoInt, err := strconv.Atoi(pageNo)
		if err != nil {
			return nil, fmt.Errorf("invalid pageNo parameter: %s", pageNo)
		}
		transformRequest.PageNo = pageNoInt
	}
	requestOptions.TransformOptions = transformRequest

	return requestOptions, nil
}
