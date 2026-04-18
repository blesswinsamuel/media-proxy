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

// TestGetMetadata_BasicDimensions tests that the metadata endpoint returns
// correct dimensions and format for a PNG image.
func TestGetMetadata_BasicDimensions(t *testing.T) {
	resp := doRequest(t, "metadata", "test.png", nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var meta mediaprocessor.MetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("failed to decode metadata response: %v", err)
	}
	if meta.Width != 400 {
		t.Errorf("expected width 400, got %d", meta.Width)
	}
	if meta.Height != 300 {
		t.Errorf("expected height 300, got %d", meta.Height)
	}
	if meta.Format == "" {
		t.Error("expected non-empty format field")
	}
}

// TestGetMetadata_WithThumbHash tests that the metadata endpoint returns
// a thumbhash when requested.
func TestGetMetadata_WithThumbHash(t *testing.T) {
	params := url.Values{"thumbhash": []string{"true"}}
	resp := doRequest(t, "metadata", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}

	var meta mediaprocessor.MetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("failed to decode metadata response: %v", err)
	}
	if meta.Thumbhash == "" {
		t.Error("expected non-empty thumbhash field")
	}
}

// TestGetMetadata_WithBlurHash tests that the metadata endpoint returns
// a blurhash when requested.
func TestGetMetadata_WithBlurHash(t *testing.T) {
	params := url.Values{"blurhash": []string{"true"}}
	resp := doRequest(t, "metadata", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}

	var meta mediaprocessor.MetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("failed to decode metadata response: %v", err)
	}
	if meta.Blurhash == "" {
		t.Error("expected non-empty blurhash field")
	}
}

// TestTransformMedia_Raw tests that the raw transform returns the original
// image bytes unchanged.
func TestTransformMedia_Raw(t *testing.T) {
	params := url.Values{"raw": []string{"true"}, "outputFormat": []string{"png"}}
	resp := doRequest(t, "media", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	img, format, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to decode response image: %v", err)
	}
	if format != "png" {
		t.Errorf("expected png format, got %q", format)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 300 {
		t.Errorf("expected 400x300 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

// TestTransformMedia_ResizeByWidth tests resizing an image to a specific width,
// verifying that the aspect ratio is maintained.
func TestTransformMedia_ResizeByWidth(t *testing.T) {
	params := url.Values{
		"resize.width": []string{"200"},
		"outputFormat": []string{"png"},
	}
	resp := doRequest(t, "media", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to decode response image: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 200 {
		t.Errorf("expected width 200, got %d", bounds.Dx())
	}
	// 400x300 resized to width=200 should have height=150 (aspect ratio preserved)
	if bounds.Dy() != 150 {
		t.Errorf("expected height 150, got %d", bounds.Dy())
	}
}

// TestTransformMedia_ResizeByHeight tests resizing an image to a specific height.
func TestTransformMedia_ResizeByHeight(t *testing.T) {
	params := url.Values{
		"resize.height": []string{"150"},
		"outputFormat":  []string{"png"},
	}
	resp := doRequest(t, "media", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to decode response image: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dy() != 150 {
		t.Errorf("expected height 150, got %d", bounds.Dy())
	}
	// 400x300 resized to height=150 should have width=200 (aspect ratio preserved)
	if bounds.Dx() != 200 {
		t.Errorf("expected width 200, got %d", bounds.Dx())
	}
}

// TestTransformMedia_ConvertToJPEG tests that a PNG image is correctly
// converted to JPEG format.
func TestTransformMedia_ConvertToJPEG(t *testing.T) {
	params := url.Values{"outputFormat": []string{"jpeg"}}
	resp := doRequest(t, "media", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if _, _, err := image.Decode(bytes.NewReader(body)); err != nil {
		t.Errorf("response body is not a valid image: %v", err)
	}
}

// TestTransformMedia_ConvertToWebP tests that a PNG image is correctly
// converted to WebP format.
func TestTransformMedia_ConvertToWebP(t *testing.T) {
	params := url.Values{"outputFormat": []string{"webp"}}
	resp := doRequest(t, "media", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/webp" {
		t.Errorf("expected Content-Type image/webp, got %q", ct)
	}
}

// TestTransformMedia_ResizeAndConvert tests resizing and format conversion in one request.
func TestTransformMedia_ResizeAndConvert(t *testing.T) {
	params := url.Values{
		"resize.width": []string{"100"},
		"outputFormat": []string{"jpeg"},
	}
	resp := doRequest(t, "media", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to decode response image: %v", err)
	}
	if img.Bounds().Dx() != 100 {
		t.Errorf("expected width 100, got %d", img.Bounds().Dx())
	}
}

// TestTransformMedia_ContentNegotiation tests that the Accept header is used
// to determine the output format when outputFormat is not specified.
func TestTransformMedia_ContentNegotiation(t *testing.T) {
	headers := http.Header{"Accept": []string{"image/webp,image/*,*/*"}}
	resp := doRequest(t, "media", "test.png", nil, headers)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/webp" {
		t.Errorf("expected Content-Type image/webp via content negotiation, got %q", ct)
	}
}

// TestTransformMedia_JPEGSource tests transforming a JPEG source image.
func TestTransformMedia_JPEGSource(t *testing.T) {
	params := url.Values{
		"resize.width": []string{"200"},
		"outputFormat": []string{"jpeg"},
	}
	resp := doRequest(t, "media", "test.jpg", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %q", ct)
	}
}

// TestTransformMedia_CacheControlHeader tests that the Cache-Control header
// is set to immutable for transformed images.
func TestTransformMedia_CacheControlHeader(t *testing.T) {
	params := url.Values{"outputFormat": []string{"png"}}
	resp := doRequest(t, "media", "test.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("unexpected Cache-Control header: %q", cc)
	}
}

// TestInvalidSignature tests that a request with an invalid signature
// is rejected by the server.
func TestInvalidSignature(t *testing.T) {
	u := fmt.Sprintf("%s/%s/media/test.png?outputFormat=png", testServerURL, "invalidsignaturehere=")
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 status for invalid signature, got 200")
	}
}

// TestNonExistentImage tests that requesting a non-existent image
// returns an error response.
func TestNonExistentImage(t *testing.T) {
	params := url.Values{"outputFormat": []string{"png"}}
	resp := doRequest(t, "media", "notfound.png", params, nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 status for non-existent image, got 200")
	}
}

// TestGetMetadata_NonExistentImage tests that requesting metadata for a
// non-existent image returns an error.
func TestGetMetadata_NonExistentImage(t *testing.T) {
	resp := doRequest(t, "metadata", "notfound.png", nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 status for non-existent image, got 200")
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
