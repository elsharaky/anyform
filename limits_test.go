package anyform

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

type bodyLimitStruct struct {
	Name string `form:"name"`
}

func buildBodyMultipart(t *testing.T, field, content string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, "x.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

func TestWithMaxBodySize_WithinLimit(t *testing.T) {
	body := []byte("name=John")
	var v bodyLimitStruct
	if err := Unmarshal(body, "application/x-www-form-urlencoded", &v, WithMaxBodySize(100)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "John" {
		t.Errorf("name = %q, want John", v.Name)
	}
}

func TestWithMaxBodySize_ExceedsLimit(t *testing.T) {
	body := []byte("name=John&city=NYC")
	var v bodyLimitStruct
	err := Unmarshal(body, "application/x-www-form-urlencoded", &v, WithMaxBodySize(10))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

func TestWithMaxBodySize_ZeroMeansUnlimited(t *testing.T) {
	body := []byte("name=John")
	var v bodyLimitStruct
	if err := Unmarshal(body, "application/x-www-form-urlencoded", &v, WithMaxBodySize(0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "John" {
		t.Errorf("name = %q", v.Name)
	}
}

type fileLimitStruct struct {
	Doc File
}

func TestWithMaxFileSize_WithinLimit(t *testing.T) {
	body, ct := buildBodyMultipart(t, "Doc", "small content")
	var v fileLimitStruct
	if err := Unmarshal(body, ct, &v, WithMaxFileSize(1024)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(v.Doc.Content) != "small content" {
		t.Errorf("content = %q", v.Doc.Content)
	}
}

func TestWithMaxFileSize_ExceedsLimit(t *testing.T) {
	body, ct := buildBodyMultipart(t, "Doc", "big file content here")
	var v fileLimitStruct
	err := Unmarshal(body, ct, &v, WithMaxFileSize(5))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestWithMaxFileSize_NoLimitByDefault(t *testing.T) {
	body, ct := buildBodyMultipart(t, "Doc", "this content is quite long and would exceed a small limit")
	var v fileLimitStruct
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithMaxFileSize_DecoderPath(t *testing.T) {
	body, ct := buildBodyMultipart(t, "Doc", "some content")
	dec := NewDecoder(WithMaxFileSize(3))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse multipart: %v", err)
	}

	var v fileLimitStruct
	err := dec.UnmarshalMultipartForm(req.MultipartForm, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

type multiFileLimitStruct struct {
	Docs []File
}

func TestWithMaxFileSize_MultiFileRejectsWholeInput(t *testing.T) {
	body, ct := buildBodyMultipart(t, "Docs", "oversized file content")
	var v multiFileLimitStruct
	err := Unmarshal(body, ct, &v, WithMaxFileSize(4))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}
