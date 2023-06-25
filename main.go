package main

import (
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/blesswinsamuel/media-proxy/cache"
	"github.com/blesswinsamuel/media-proxy/mediaprocessor"
	"github.com/blesswinsamuel/media-proxy/server"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/rs/zerolog/log"
)

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
	metricsPort := getEnv("METRICS_PORT", "8081")
	port := getEnv("PORT", "8080")
	secret := getEnv("SECRET", "")
	if !enableUnsafe {
		if secret == "" {
			log.Fatal().Msg("SECRET must be set when ENABLE_UNSAFE=false")
		}
	}

	originalCache := cache.NewFsCache(cachePath + "/original")
	metadataCache := cache.NewFsCache(cachePath + "/metadata")

	mediaProcessor := mediaprocessor.NewMediaProcessor(originalCache)

	server := server.NewServer(server.ServerConfig{
		Port:         port,
		MetricsPort:  metricsPort,
		BaseURL:      baseURL,
		Secret:       secret,
		EnableUnsafe: enableUnsafe,
		AutoAvif:     true,
		AutoWebp:     true,
	}, mediaProcessor, metadataCache)

	// Start the server
	server.Start()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info().Msg("Shutting down...")
	server.Stop()
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
