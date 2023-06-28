package mediaprocessor

import (
	"os"
	"testing"

	"github.com/davidbyttow/govips/v2/vips"
)

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
	imageBytes, err := os.ReadFile("testdata/christmas-tree.jpg")
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
