package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/davidbyttow/govips/v2/vips"
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
	baseURLParsed, err := url.Parse(baseURL)
	if err != nil {
		log.Fatal("Failed to parse BASE_URL:", err)
	}

	cache := &FsCache{
		cachePath: getEnv("CACHE_PATH", "/tmp/cache"),
	}

	mediaProcessor := &MediaProcessor{
		cache: cache,
	}

	server := &server{
		baseURL:        baseURLParsed,
		enableUnsafe:   getEnv("ENABLE_UNSAFE", "false") == "true",
		mediaProcessor: mediaProcessor,
	}

	// Start the server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		server.handleRequest(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default port
	}

	go func() {
		log.Printf("Server listening on port %s", port)
		log.Fatal(http.ListenAndServe(":"+port, nil))
	}()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("Shutting down...")
}

type server struct {
	baseURL        *url.URL
	enableUnsafe   bool
	mediaProcessor *MediaProcessor
	autoAvif       bool
	autoWebp       bool
}

func (s *server) handleRequest(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(r.URL.Path, "/", 3)
	if len(parts) != 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	signature := parts[1]
	imagePath := parts[2]

	if !s.enableUnsafe {
		if !validateSignature(signature, imagePath) {
			http.Error(w, "Invalid signature", http.StatusForbidden)
			return
		}
	}

	upstreamURL, err := url.Parse(fmt.Sprintf("%s/%s", s.baseURL.String(), imagePath))
	if err != nil {
		http.Error(w, "Failed to parse upstream URL", http.StatusInternalServerError)
		log.Println("Failed to parse upstream URL:", err)
		return
	}

	// Perform the request to the target server
	imageBytes, err := s.mediaProcessor.fetchMedia(r.Context(), upstreamURL)
	if err != nil {
		http.Error(w, "Failed to fetch image", http.StatusInternalServerError)
		log.Println("Failed to fetch image:", err)
		return
	}

	transformOptions, err := parseQuery(r.URL.Query())
	if err != nil {
		http.Error(w, "Failed to parse query", http.StatusBadRequest)
		log.Println("Failed to parse query:", err)
		return
	}

	// contentType := resp.Header.Get("Content-Type")
	// fmt.Println(contentType)
	// if strings.HasPrefix(contentType, "image/") {
	// Transform the image using libvips
	bytes, contentType, err := s.mediaProcessor.processMedia(imageBytes, *transformOptions)
	if err != nil {
		log.Println("Image transformation failed:", err)
		http.Error(w, "Image transformation failed", http.StatusInternalServerError)
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

func parseQuery(query url.Values) (*TransformOptions, error) {
	transformOptions := &TransformOptions{}

	resizeParam := query.Get("resize")
	if resizeParam != "" {
		var width, height int
		_, err := fmt.Sscanf(resizeParam, "%dx%d", &width, &height)
		if err != nil {
			return nil, fmt.Errorf("invalid resize parameter: %s", resizeParam)
		}
		transformOptions.Resize = &TransformOptionsResize{Width: width, Height: height}
	}

	return transformOptions, nil
}
