package anyform

import (
	"mime"
	"testing"
)

func TestMarshalMultipartNoFileFields(t *testing.T) {
	type simple struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
	}

	enc := NewEncoder()
	body, ct, err := enc.MarshalMultipart(simple{Name: "x", Age: 3})
	if err != nil {
		t.Fatalf("MarshalMultipart: %v", err)
	}

	// It must be a valid multipart body and round-trip back.
	var out simple
	if err := Unmarshal(body, ct, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Name != "x" || out.Age != 3 {
		t.Errorf("round-trip: got %+v", out)
	}
}

func TestUnmarshalMultipartWithoutBoundary(t *testing.T) {
	var out unifiedReq
	err := Unmarshal([]byte("name=bob"), "multipart/form-data", &out)
	if err == nil {
		t.Fatal("expected error for missing boundary")
	}
}

func TestUnmarshalURLEncodedHttpCompatible(t *testing.T) {
	// A typical Content-Type from a browser form post.
	var out unifiedReq
	body := []byte("name=carol&age=40")
	if err := Unmarshal(body, "application/x-www-form-urlencoded; charset=UTF-8", &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Name != "carol" || out.Age != 40 {
		t.Errorf("got %+v", out)
	}
}

func TestMarshalMultipartFormDataContentTypeBoundary(t *testing.T) {
	// Ensure the emitted Content-Type parses to multipart/form-data with a boundary.
	body, ct, err := Marshal(unifiedUpload{Avatar: File{Filename: "photo.png", Content: []byte("x")}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("parse ct: %v", err)
	}
	if mt != "multipart/form-data" {
		t.Errorf("media type = %q", mt)
	}
	if params["boundary"] == "" {
		t.Error("missing boundary")
	}
	var out unifiedUpload
	if err := Unmarshal(body, ct, &out); err != nil {
		t.Fatalf("unmarshal round-trip: %v", err)
	}
}
