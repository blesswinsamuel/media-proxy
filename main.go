package main

import (
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/blesswinsamuel/media-proxy/internal/cache"
	"github.com/blesswinsamuel/media-proxy/internal/config"
	"github.com/blesswinsamuel/media-proxy/internal/loader"
	"github.com/blesswinsamuel/media-proxy/internal/mediaprocessor"
	"github.com/blesswinsamuel/media-proxy/internal/server"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// NewLogger creates a new logger based on the current configuration
func NewLogger(logLevel string) zerolog.Logger {
	// Setup logger
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ll, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		ll = zerolog.DebugLevel
	}
	return log.With().Logger().Level(ll)
}

func main() {
	config, err := config.ParseConfig(nil)
	if err != nil {
		log := NewLogger(zerolog.DebugLevel.String())
		log.Fatal().Err(err).Msg("failed to parse config")
	}

	// Setup logger
	log := NewLogger(config.LogLevel)

	// Perform config validation
	config.Validate()

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
		// https://www.libvips.org/API/current/VipsOperation.html#vips-concurrency-set
		ConcurrencyLevel: 4,
		// https://www.libvips.org/API/current/VipsOperation.html#vips-cache-set-max-files
		MaxCacheFiles: 0,
		// https://www.libvips.org/API/current/VipsOperation.html#vips-cache-set-max-mem
		MaxCacheMem: 50 * 1024 * 1024,
		// https://www.libvips.org/API/current/VipsOperation.html#vips-cache-set-max
		MaxCacheSize: 100,
		// https://www.libvips.org/API/current/libvips-vips.html#vips-leak-set
		ReportLeaks: true,
		// https://www.libvips.org/API/current/VipsOperation.html#vips-cache-set-trace
		// CacheTrace       :
		CollectStats: true,
	})
	defer vips.Shutdown()

	prometheus.MustRegister(mediaprocessor.NewVipsPrometheusCollector())

	// go func() {
	// 	for {
	// 		// runtimeStats := vips.RuntimeStats{}
	// 		// vips.ReadRuntimeStats(&runtimeStats)
	// 		// fmt.Println(runtimeStats)

	// 		// memoryStats := vips.MemoryStats{}
	// 		// vips.ReadVipsMemStats(&memoryStats)
	// 		// fmt.Println(memoryStats)

	// 		time.Sleep(5 * time.Second)

	// 		vips.PrintObjectReport("main")
	// 	}
	// }()
	var loaderCache, metadataCache, resultCache cache.Cache
	if config.EnableLoaderCache.Value {
		loaderCache = cache.NewFsCache(path.Join(config.CacheDir, "original"))
	} else {
		loaderCache = cache.NewNoopCache()
	}
	if config.EnableResultCache.Value {
		metadataCache = cache.NewFsCache(path.Join(config.CacheDir, "metadata"))
		resultCache = cache.NewFsCache(path.Join(config.CacheDir, "result"))
	} else {
		metadataCache = cache.NewNoopCache()
		resultCache = cache.NewNoopCache()
	}

	mediaProcessor := mediaprocessor.NewMediaProcessor()
	loader := loader.NewHTTPLoader(config.BaseURL)

	server := server.NewServer(server.ServerConfig{
		Port:         config.Port,
		MetricsPort:  config.MetricsPort,
		Secret:       config.Secret,
		EnableUnsafe: bool(config.EnableUnsafe.Value),
		AutoAvif:     true,
		AutoWebp:     true,
		Concurrency:  config.Concurrency,
	}, mediaProcessor, loader, loaderCache, metadataCache, resultCache)

	// Start the server
	server.Start()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info().Msg("Shutting down...")
	server.Stop()
}
