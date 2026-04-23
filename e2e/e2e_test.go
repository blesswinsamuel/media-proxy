package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/blesswinsamuel/media-proxy/internal/cache"
	"github.com/blesswinsamuel/media-proxy/internal/loader"
	"github.com/blesswinsamuel/media-proxy/internal/mediaprocessor"
	"github.com/blesswinsamuel/media-proxy/internal/server"
	"github.com/davidbyttow/govips/v2/vips"
)

const testSecret = "test-e2e-secret"

var testServerURL string

func TestMain(m *testing.M) {
	os.Exit(setup(m))
}

func setup(m *testing.M) int {
	vips.LoggingSettings(nil, vips.LogLevelWarning)
	vips.Startup(&vips.Config{
		ConcurrencyLevel: 2,
		MaxCacheFiles:    0,
		MaxCacheMem:      50 * 1024 * 1024,
		MaxCacheSize:     100,
	})
	defer vips.Shutdown()

	upstreamMux := http.NewServeMux()
	upstreamMux.HandleFunc("/test.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(createTestPNG(400, 300))
	})
	upstreamMux.HandleFunc("/test.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(createTestJPEG(400, 300))
	})
	upstreamMux.HandleFunc("/notfound.png", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	upstream := httptest.NewServer(upstreamMux)
	defer upstream.Close()

	mainPort, err := getFreePort()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get free port: %v\n", err)
		return 1
	}
	metricsPort, err := getFreePort()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get free port: %v\n", err)
		return 1
	}
	testServerURL = fmt.Sprintf("http://localhost:%d", mainPort)

	mp := mediaprocessor.NewMediaProcessor()
	l := loader.NewHTTPLoader(upstream.URL + "/")
	noopCache := cache.NewNoopCache()
	srv := server.NewServer(server.ServerConfig{
		Port:        strconv.Itoa(mainPort),
		MetricsPort: strconv.Itoa(metricsPort),
		Secret:      testSecret,
		Concurrency: 4,
	}, mp, l, noopCache, noopCache, noopCache)
	srv.Start()
	defer srv.Stop()

	if err := waitForServer(testServerURL, 5*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "server did not start: %v\n", err)
		return 1
	}

	return m.Run()
}

func TestMetadata(t *testing.T) {
	tests := []struct {
		name        string
		mediaPath   string
		params      url.Values
		wantStatus  int
		checkResult func(t *testing.T, meta mediaprocessor.MetadataResponse)
	}{
		{
			name:       "basic dimensions",
			mediaPath:  "test.png",
			wantStatus: http.StatusOK,
			checkResult: func(t *testing.T, meta mediaprocessor.MetadataResponse) {
				if meta.Width != 400 {
					t.Errorf("expected width 400, got %d", meta.Width)
				}
				if meta.Height != 300 {
					t.Errorf("expected height 300, got %d", meta.Height)
				}
				if meta.Format == "" {
					t.Error("expected non-empty format field")
				}
			},
		},
		{
			name:       "thumbhash",
			mediaPath:  "test.png",
			params:     url.Values{"thumbhash": []string{"true"}},
			wantStatus: http.StatusOK,
			checkResult: func(t *testing.T, meta mediaprocessor.MetadataResponse) {
				if meta.Thumbhash == "" {
					t.Error("expected non-empty thumbhash field")
				}
			},
		},
		{
			name:       "blurhash",
			mediaPath:  "test.png",
			params:     url.Values{"blurhash": []string{"true"}},
			wantStatus: http.StatusOK,
			checkResult: func(t *testing.T, meta mediaprocessor.MetadataResponse) {
				if meta.Blurhash == "" {
					t.Error("expected non-empty blurhash field")
				}
			},
		},
		{
			name:       "not found",
			mediaPath:  "notfound.png",
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, "metadata", tc.mediaPath, tc.params, nil)
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, resp.StatusCode, body)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", ct)
			}
			if tc.checkResult != nil {
				var meta mediaprocessor.MetadataResponse
				if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
					t.Fatalf("failed to decode metadata response: %v", err)
				}
				tc.checkResult(t, meta)
			}
		})
	}
}

func TestTransformMedia(t *testing.T) {
	tests := []struct {
		name        string
		mediaPath   string
		params      url.Values
		headers     http.Header
		wantStatus  int
		wantCT      string
		checkImage  func(t *testing.T, img image.Image, format string)
		checkHeader func(t *testing.T, h http.Header)
	}{
		{
			name:       "raw passthrough",
			mediaPath:  "test.png",
			params:     url.Values{"raw": []string{"true"}, "outputFormat": []string{"png"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/png",
			checkImage: func(t *testing.T, img image.Image, format string) {
				if format != "png" {
					t.Errorf("expected png format, got %q", format)
				}
				if b := img.Bounds(); b.Dx() != 400 || b.Dy() != 300 {
					t.Errorf("expected 400x300, got %dx%d", b.Dx(), b.Dy())
				}
			},
		},
		{
			name:      "resize by width",
			mediaPath: "test.png",
			params:    url.Values{"resize.width": []string{"200"}, "outputFormat": []string{"png"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/png",
			checkImage: func(t *testing.T, img image.Image, _ string) {
				// 400x300 → width=200 preserves aspect ratio → 200x150
				if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 150 {
					t.Errorf("expected 200x150, got %dx%d", b.Dx(), b.Dy())
				}
			},
		},
		{
			name:      "resize by height",
			mediaPath: "test.png",
			params:    url.Values{"resize.height": []string{"150"}, "outputFormat": []string{"png"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/png",
			checkImage: func(t *testing.T, img image.Image, _ string) {
				// 400x300 → height=150 preserves aspect ratio → 200x150
				if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 150 {
					t.Errorf("expected 200x150, got %dx%d", b.Dx(), b.Dy())
				}
			},
		},
		{
			name:       "convert to jpeg",
			mediaPath:  "test.png",
			params:     url.Values{"outputFormat": []string{"jpeg"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/jpeg",
		},
		{
			name:       "convert to webp",
			mediaPath:  "test.png",
			params:     url.Values{"outputFormat": []string{"webp"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/webp",
		},
		{
			name:      "resize and convert",
			mediaPath: "test.png",
			params:    url.Values{"resize.width": []string{"100"}, "outputFormat": []string{"jpeg"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/jpeg",
			checkImage: func(t *testing.T, img image.Image, _ string) {
				if img.Bounds().Dx() != 100 {
					t.Errorf("expected width 100, got %d", img.Bounds().Dx())
				}
			},
		},
		{
			name:       "content negotiation webp",
			mediaPath:  "test.png",
			headers:    http.Header{"Accept": []string{"image/webp,image/*,*/*"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/webp",
		},
		{
			name:      "jpeg source",
			mediaPath: "test.jpg",
			params:    url.Values{"resize.width": []string{"200"}, "outputFormat": []string{"jpeg"}},
			wantStatus: http.StatusOK,
			wantCT:     "image/jpeg",
		},
		{
			name:      "cache-control header",
			mediaPath: "test.png",
			params:    url.Values{"outputFormat": []string{"png"}},
			wantStatus: http.StatusOK,
			checkHeader: func(t *testing.T, h http.Header) {
				if cc := h.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
					t.Errorf("unexpected Cache-Control: %q", cc)
				}
			},
		},
		{
			name:       "invalid signature",
			mediaPath:  "", // handled inline below via wantStatus
			wantStatus: http.StatusInternalServerError, // sentinel; handled specially
		},
		{
			name:       "not found",
			mediaPath:  "notfound.png",
			params:     url.Values{"outputFormat": []string{"png"}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp *http.Response
			if tc.name == "invalid signature" {
				u := fmt.Sprintf("%s/%s/media/test.png?outputFormat=png", testServerURL, "invalidsignaturehere=")
				var err error
				resp, err = http.Get(u)
				if err != nil {
					t.Fatalf("request failed: %v", err)
				}
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					t.Error("expected non-200 status for invalid signature, got 200")
				}
				return
			}

			resp = doRequest(t, "media", tc.mediaPath, tc.params, tc.headers)
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, resp.StatusCode, body)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			if tc.wantCT != "" {
				if ct := resp.Header.Get("Content-Type"); ct != tc.wantCT {
					t.Errorf("expected Content-Type %q, got %q", tc.wantCT, ct)
				}
			}
			if tc.checkHeader != nil {
				tc.checkHeader(t, resp.Header)
			}
			if tc.checkImage != nil {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("failed to read response body: %v", err)
				}
				img, format, err := image.Decode(bytes.NewReader(body))
				if err != nil {
					t.Fatalf("failed to decode response image: %v", err)
				}
				tc.checkImage(t, img, format)
			}
		})
	}
}

// --- helpers ---

// doRequest constructs a properly signed request to the media proxy and returns the response.
func doRequest(t *testing.T, requestType, mediaPath string, queryParams url.Values, headers http.Header) *http.Response {
	t.Helper()
	rawQuery := queryParams.Encode()
	sig := signRequest(testSecret, requestType, mediaPath, rawQuery)

	u := fmt.Sprintf("%s/%s/%s/%s", testServerURL, sig, requestType, mediaPath)
	if rawQuery != "" {
		u += "?" + rawQuery
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request to %s failed: %v", u, err)
	}
	return resp
}

// signRequest computes HMAC-SHA1 over "<requestType>/<mediaPath>[?<rawQuery>]".
func signRequest(secret, requestType, mediaPath, rawQuery string) string {
	path := requestType + "/" + mediaPath
	if rawQuery != "" {
		path = path + "?" + rawQuery
	}
	h := hmac.New(sha1.New, []byte(secret))
	h.Write([]byte(path))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

// getFreePort asks the OS for a free TCP port.
func getFreePort() (int, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitForServer polls the server until it responds or the timeout is reached.
func waitForServer(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		// A connection-refused error means not ready; any actual HTTP response means ready.
		// chi returns 404 for unknown routes, which is still a valid server response.
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server at %s did not become ready within %v", baseURL, timeout)
}

// createTestPNG creates an in-memory PNG image of the given dimensions.
func createTestPNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / width),
				G: uint8(y * 255 / height),
				B: 100,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(fmt.Sprintf("failed to encode test PNG: %v", err))
	}
	return buf.Bytes()
}

// createTestJPEG creates an in-memory JPEG image of the given dimensions.
func createTestJPEG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: 100,
				G: uint8(x * 255 / width),
				B: uint8(y * 255 / height),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic(fmt.Sprintf("failed to encode test JPEG: %v", err))
	}
	return buf.Bytes()
}
