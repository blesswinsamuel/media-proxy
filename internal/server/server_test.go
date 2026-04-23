package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"net/url"
	"reflect"
	"testing"

	"github.com/blesswinsamuel/media-proxy/internal/mediaprocessor"
)

func buildEncodedBytes(contentType string, data []byte) []byte {
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(len(contentType)))
	result := append(sizeBytes, []byte(contentType)...)
	return append(result, data...)
}

func TestConcatenateContentTypeAndData(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		data        []byte
	}{
		{"json with text data", "application/json", []byte(`{"key":"value"}`)},
		{"image/jpeg with binary data", "image/jpeg", []byte{0xFF, 0xD8, 0xFF}},
		{"empty data", "image/png", []byte{}},
		{"empty content type", "", []byte("some data")},
		{"empty both", "", []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := buildEncodedBytes(tt.contentType, tt.data)
			got := concatenateContentTypeAndData(tt.contentType, tt.data)
			if !bytes.Equal(got, want) {
				t.Errorf("concatenateContentTypeAndData(%q, %v) = %v, want %v", tt.contentType, tt.data, got, want)
			}
		})
	}
}

func TestGetContentTypeAndData(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		data        []byte
	}{
		{"json with text data", "application/json", []byte(`testdata`)},
		{"image/jpeg with binary data", "image/jpeg", []byte{0x89, 0x50, 0x4E, 0x47}},
		{"empty data", "image/png", []byte{}},
		{"empty content type", "", []byte("some data")},
		{"empty both", "", []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := buildEncodedBytes(tt.contentType, tt.data)
			gotContentType, gotData := getContentTypeAndData(input)
			if gotContentType != tt.contentType {
				t.Errorf("getContentTypeAndData content type = %q, want %q", gotContentType, tt.contentType)
			}
			if !bytes.Equal(gotData, tt.data) {
				t.Errorf("getContentTypeAndData data = %v, want %v", gotData, tt.data)
			}
		})
	}
}

func computeSignature(secret, path string) string {
	h := hmac.New(sha1.New, []byte(secret))
	h.Write([]byte(path))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

func TestValidateSignature(t *testing.T) {
	const secret = "test-secret"
	s := &server{config: ServerConfig{Secret: secret}}

	tests := []struct {
		name      string
		signature string
		path      string
		want      bool
	}{
		{
			name:      "valid signature",
			signature: computeSignature(secret, "media/path/to/image.jpg"),
			path:      "media/path/to/image.jpg",
			want:      true,
		},
		{
			name:      "wrong signature",
			signature: "invalidsignature==",
			path:      "media/path/to/image.jpg",
			want:      false,
		},
		{
			name:      "empty signature",
			signature: "",
			path:      "media/path/to/image.jpg",
			want:      false,
		},
		{
			name:      "tampered path",
			signature: computeSignature(secret, "media/path/to/image.jpg"),
			path:      "media/different/image.jpg",
			want:      false,
		},
		{
			name:      "empty path with matching signature",
			signature: computeSignature(secret, ""),
			path:      "",
			want:      true,
		},
		{
			name:      "signature computed with different secret",
			signature: computeSignature("other-secret", "media/path/to/image.jpg"),
			path:      "media/path/to/image.jpg",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.validateSignature(tt.signature, tt.path)
			if got != tt.want {
				t.Errorf("validateSignature(%q, %q) = %v, want %v", tt.signature, tt.path, got, tt.want)
			}
		})
	}
}

func TestParseTransformQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    *mediaprocessor.TransformOptions
		wantErr bool
	}{
		{
			name:  "empty query",
			query: "",
			want:  &mediaprocessor.TransformOptions{},
		},
		{
			name:  "output format",
			query: "outputFormat=webp",
			want:  &mediaprocessor.TransformOptions{OutputFormat: "webp"},
		},
		{
			name:  "raw mode",
			query: "raw=true",
			want:  &mediaprocessor.TransformOptions{Raw: true},
		},
		{
			name:  "resize width and height",
			query: "resize.width=800&resize.height=600",
			want: &mediaprocessor.TransformOptions{
				Resize: &mediaprocessor.TransformOptionsResize{Width: 800, Height: 600},
			},
		},
		{
			name:  "resize with crop and size hint",
			query: "resize.width=100&resize.crop=attention&resize.size=down",
			want: &mediaprocessor.TransformOptions{
				Resize: &mediaprocessor.TransformOptionsResize{Width: 100, Crop: "attention", Size: "down"},
			},
		},
		{
			name:  "read dpi and page",
			query: "read.dpi=150&read.page=3",
			want: &mediaprocessor.TransformOptions{
				Read: mediaprocessor.ReadOptions{Dpi: 150, Page: 3},
			},
		},
		{
			name:    "invalid width value",
			query:   "resize.width=notanumber",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("url.ParseQuery(%q) error: %v", tt.query, err)
			}
			got, err := parseTransformQuery(values)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTransformQuery(%q) error = %v, wantErr %v", tt.query, err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTransformQuery(%q) = %+v, want %+v", tt.query, got, tt.want)
			}
		})
	}
}

func TestParseMetadataQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    *mediaprocessor.MetadataOptions
		wantErr bool
	}{
		{
			name:  "empty query",
			query: "",
			want:  &mediaprocessor.MetadataOptions{},
		},
		{
			name:  "thumbhash enabled",
			query: "thumbhash=true",
			want:  &mediaprocessor.MetadataOptions{ThumbHash: true},
		},
		{
			name:  "blurhash enabled",
			query: "blurhash=true",
			want:  &mediaprocessor.MetadataOptions{BlurHash: true},
		},
		{
			name:  "potatowebp enabled",
			query: "potatowebp=true",
			want:  &mediaprocessor.MetadataOptions{PotatoWebp: true},
		},
		{
			name:  "all hash options",
			query: "thumbhash=true&blurhash=true&potatowebp=true",
			want:  &mediaprocessor.MetadataOptions{ThumbHash: true, BlurHash: true, PotatoWebp: true},
		},
		{
			name:  "read dpi and page",
			query: "read.dpi=300&read.page=2",
			want:  &mediaprocessor.MetadataOptions{Read: mediaprocessor.ReadOptions{Dpi: 300, Page: 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("url.ParseQuery(%q) error: %v", tt.query, err)
			}
			got, err := parseMetadataQuery(values)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMetadataQuery(%q) error = %v, wantErr %v", tt.query, err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseMetadataQuery(%q) = %+v, want %+v", tt.query, got, tt.want)
			}
		})
	}
}
