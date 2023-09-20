package mediaprocessor

import (
	"errors"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/davidbyttow/govips/v2/vips"
)

func downloadFile(url, fileName string) error {
	//Get the response bytes from the url
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return errors.New("Received non 200 response code")
	}
	//Create a empty file
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	//Write the bytes to the fiel
	_, err = io.Copy(file, response.Body)
	if err != nil {
		return err
	}

	return nil
}

func BenchmarkProcessMetadataRequest(b *testing.B) {
	vips.LoggingSettings(nil, vips.LogLevelWarning)
	vips.Startup(&vips.Config{
		ConcurrencyLevel: 1,
		MaxCacheFiles:    0,
		MaxCacheMem:      50 * 1024 * 1024,
		MaxCacheSize:     100,
		// ReportLeaks      :
		// CacheTrace       :
		// CollectStats     :
	})
	// defer vips.Shutdown()

	// Load test image
	if err := os.Mkdir("tempdata", 0755); err != nil {
		if !os.IsExist(err) {
			b.Fatalf("failed to create tempdata directory: %v", err)
		}
	}
	if _, err := os.Stat("tempdata/christmas-tree.jpg"); os.IsNotExist(err) {
		err := downloadFile("https://wallpaperswide.com/download/small_christmas_tree-wallpaper-2560x1600.jpg", "tempdata/christmas-tree.jpg")
		if err != nil {
			b.Fatalf("failed to download test image: %v", err)
		}
	}
	imageBytes, err := os.ReadFile("tempdata/christmas-tree.jpg")
	if err != nil {
		b.Fatalf("failed to read test image: %v", err)
	}

	// Create metadata options
	params := &MetadataOptions{
		// BlurHash:  true,
		ThumbHash: true,
		// PotatoWebp: true,
	}

	mp := NewMediaProcessor()
	// Run the benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := mp.ProcessMetadataRequest(imageBytes, params)
		if err != nil {
			b.Fatalf("failed to process metadata: %v", err)
		}
	}
}
