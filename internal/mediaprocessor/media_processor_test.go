package mediaprocessor

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/davidbyttow/govips/v2/vips"
)

func TestParseVipsInteresting(t *testing.T) {
	tests := []struct {
		input   string
		want    vips.Interesting
		wantErr bool
	}{
		{"", vips.InterestingNone, false},
		{"none", vips.InterestingNone, false},
		{"centre", vips.InterestingCentre, false},
		{"entropy", vips.InterestingEntropy, false},
		{"attention", vips.InterestingAttention, false},
		{"low", vips.InterestingLow, false},
		{"high", vips.InterestingHigh, false},
		{"all", vips.InterestingAll, false},
		{"last", vips.InterestingLast, false},
		{"invalid", 0, true},
		{"CENTER", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseVipsInteresting(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseVipsInteresting(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseVipsInteresting(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseVipsSize(t *testing.T) {
	tests := []struct {
		input   string
		want    vips.Size
		wantErr bool
	}{
		{"", vips.SizeBoth, false},
		{"both", vips.SizeBoth, false},
		{"up", vips.SizeUp, false},
		{"down", vips.SizeDown, false},
		{"force", vips.SizeForce, false},
		{"last", vips.SizeLast, false},
		{"invalid", 0, true},
		{"DOWN", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseVipsSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseVipsSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseVipsSize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func makeTestJPEG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func BenchmarkProcessMetadataRequest(b *testing.B) {
	vips.LoggingSettings(nil, vips.LogLevelWarning)
	vips.Startup(&vips.Config{
		ConcurrencyLevel: 1,
		MaxCacheFiles:    0,
		MaxCacheMem:      50 * 1024 * 1024,
		MaxCacheSize:     100,
	})

	imageBytes := makeTestJPEG(2560, 1600)

	// Create metadata options
	params := &MetadataOptions{
		ThumbHash: true,
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
