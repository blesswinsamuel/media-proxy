package server

import (
	"bytes"
	"testing"
)

func TestConcatenateContentTypeAndData(t *testing.T) {
	contentType := "application/json"
	data := []byte(`testdata`)
	expected := append(
		[]byte{0x10, 0x00, 0x00, 0x00},
		append(
			[]byte(contentType),
			[]byte(data)...,
		)...,
	)
	result := concatenateContentTypeAndData(contentType, data)
	if !bytes.Equal(result, expected) {
		t.Errorf("concatenateContentTypeAndData(%q, %q) = %q, expected %q", contentType, data, result, expected)
	}
}

func TestGetContentTypeAndData(t *testing.T) {
	expectedContentType := "application/json"
	expectedData := []byte(`testdata`)
	concatenatedBytes := append(
		[]byte{0x10, 0x00, 0x00, 0x00},
		append(
			[]byte(expectedContentType),
			[]byte(expectedData)...,
		)...,
	)
	contentType, data := getContentTypeAndData(concatenatedBytes)
	if contentType != expectedContentType {
		t.Errorf("getContentTypeAndData(%q) returned contentType %q, expected %q", concatenatedBytes, contentType, expectedContentType)
	}
	if !bytes.Equal(data, expectedData) {
		t.Errorf("getContentTypeAndData(%q) returned data %q, expected %q", concatenatedBytes, data, expectedData)
	}
}
