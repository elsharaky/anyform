package anyform

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func buildAliasBody(t *testing.T, field, content string) ([]byte, string) {
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

// buildAliasValueBody creates a multipart body with a VALUE part (no filename).
// The multipart parser routes such parts to mf.Value, never mf.File.
func buildAliasValueBody(t *testing.T, field, content string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormField(field)
	if err != nil {
		t.Fatalf("create form field: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

// A value part applied to a File field means the client sent the part without
// a filename, so the error must say so — not the old cryptic notices.
func TestFileFieldValuePartClearError(t *testing.T) {
	type S struct {
		Avatar File `form:"avatar"`
	}

	body, ct := buildAliasValueBody(t, "avatar", "plain")
	var v S
	err := Unmarshal(body, ct, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "cannot decode value part into File field: multipart file parts must include a filename" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestFileSliceFieldValuePartClearError(t *testing.T) {
	type S struct {
		Docs []File `form:"docs"`
	}

	body, ct := buildAliasValueBody(t, "docs", "plain")
	var v S
	err := Unmarshal(body, ct, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "cannot decode value part into []File field: multipart file parts must include a filename" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestFilePtrFieldValuePartClearError(t *testing.T) {
	type S struct {
		Avatar *File `form:"avatar"`
	}

	body, ct := buildAliasValueBody(t, "avatar", "plain")
	var v S
	err := Unmarshal(body, ct, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "cannot decode value part into File field: multipart file parts must include a filename" {
		t.Fatalf("unexpected message: %q", got)
	}
}

// Bug #1: file fields must accept any tag key (form, json, xml, protobuf, Go name),
// matching how value fields behave.
func TestFileFieldAcceptsJSONTagKey(t *testing.T) {
	type S struct {
		Avatar File `json:"avatar" form:"avatar_file"`
	}

	for name, part := range map[string]string{
		"json key":    "avatar",
		"form key":    "avatar_file",
		"go name key": "Avatar",
	} {
		t.Run(name, func(t *testing.T) {
			body, ct := buildAliasBody(t, part, "hello "+part)
			var v S
			if err := Unmarshal(body, ct, &v); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if string(v.Avatar.Content) != "hello "+part {
				t.Fatalf("file content = %q, want %q", v.Avatar.Content, "hello "+part)
			}
		})
	}
}

func TestFileSliceAcceptsJSONTagKey(t *testing.T) {
	type S struct {
		Docs []File `json:"docs" form:"doc_files"`
	}

	body, ct := buildAliasBody(t, "docs", "multi-content")
	var v S
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(v.Docs) != 1 || string(v.Docs[0].Content) != "multi-content" {
		t.Fatalf("docs = %+v", v.Docs)
	}
}

func TestFilePtrAcceptsJSONTagKey(t *testing.T) {
	type S struct {
		Avatar *File `json:"avatar" form:"avatar_file"`
	}

	body, ct := buildAliasBody(t, "avatar", "ptr-content")
	var v S
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.Avatar == nil || string(v.Avatar.Content) != "ptr-content" {
		t.Fatalf("avatar = %+v", v.Avatar)
	}
}

// Bug #2: WithStrictUnmarshal must reject unknown multipart file parts,
// matching the value-field behavior.
func TestStrictUnmarshalRejectsUnknownFilePart(t *testing.T) {
	type S struct {
		Doc File `form:"doc"`
	}

	body, ct := buildAliasBody(t, "unknown", "surprise")
	var v S
	err := Unmarshal(body, ct, &v, WithStrictUnmarshal(true))
	if err == nil {
		t.Fatal("expected error for unknown file part")
	}
	var de *DecodingError
	if !errors.As(err, &de) || de.Key != "unknown" {
		t.Fatalf("expected DecodingError with Key=unknown, got %v", err)
	}
}

func TestStrictUnmarshalAcceptsKnownFilePart(t *testing.T) {
	type S struct {
		Doc File `form:"doc"`
	}

	body, ct := buildAliasBody(t, "doc", "known-content")
	var v S
	if err := Unmarshal(body, ct, &v, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(v.Doc.Content) != "known-content" {
		t.Fatalf("doc = %q", v.Doc.Content)
	}
}

func TestStrictUnmarshalAcceptsKnownFileAlias(t *testing.T) {
	type S struct {
		Doc File `json:"docx" form:"doc"`
	}

	body, ct := buildAliasBody(t, "docx", "alias-content")
	var v S
	if err := Unmarshal(body, ct, &v, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(v.Doc.Content) != "alias-content" {
		t.Fatalf("doc = %q", v.Doc.Content)
	}
}

// Strict enforcement must also work via the Decoder/UnmarshalMultipartForm path.
func TestStrictUnknownFilePartViaDecoder(t *testing.T) {
	type S struct {
		Doc File `form:"doc"`
	}

	body, ct := buildAliasBody(t, "unknown", "surprise")
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse multipart: %v", err)
	}

	dec := NewDecoder(WithStrictUnmarshal(true))
	var v S
	err := dec.UnmarshalMultipartForm(req.MultipartForm, &v)
	if err == nil {
		t.Fatal("expected error for unknown file part")
	}
	var de *DecodingError
	if !errors.As(err, &de) || de.Key != "unknown" {
		t.Fatalf("expected DecodingError with Key=unknown, got %v", err)
	}
}
