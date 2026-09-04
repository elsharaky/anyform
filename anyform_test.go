package anyform

import (
	"net/url"
	"testing"
	"time"
)

type unifiedReq struct {
	Name  string            `form:"name"`
	Age   int               `form:"age"`
	When  time.Time         `form:"when"`
	Login *bool             `form:"login"`
	Tags  []string          `form:"tags"`
	Meta  map[string]string `form:"meta"`
}

func TestMarshalURLEncoded(t *testing.T) {
	body, ct, err := Marshal(unifiedReq{Name: "Alice", Age: 30, Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Errorf("ct = %q, want urlencoded", ct)
	}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if vals.Get("name") != "Alice" || vals.Get("age") != "30" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestUnmarshalURLEncoded(t *testing.T) {
	var r unifiedReq
	body := []byte("name=Bob&age=25&tags=a&tags=b")
	if err := Unmarshal(body, "application/x-www-form-urlencoded", &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Name != "Bob" || r.Age != 25 {
		t.Errorf("got %+v", r)
	}
	if len(r.Tags) != 2 || r.Tags[0] != "a" || r.Tags[1] != "b" {
		t.Errorf("tags = %v", r.Tags)
	}
}

type unifiedUpload struct {
	Title  string `form:"title"`
	Avatar File   `form:"avatar"`
	Docs   []File `form:"docs"`
}

func TestMarshalUnmarshalMultipartRoundTrip(t *testing.T) {
	in := unifiedUpload{
		Title:  "Hello",
		Avatar: File{Filename: "a.png", ContentType: "image/png", Content: []byte("PNG")},
		Docs: []File{
			{Filename: "one.txt", ContentType: "text/plain", Content: []byte("one")},
			{Filename: "two.txt", ContentType: "text/plain", Content: []byte("two")},
		},
	}

	body, ct, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal multipart: %v", err)
	}
	if ct == "application/x-www-form-urlencoded" {
		t.Fatal("expected multipart content type")
	}

	var out unifiedUpload
	if err := Unmarshal(body, ct, &out); err != nil {
		t.Fatalf("Unmarshal multipart: %v", err)
	}

	if out.Title != "Hello" {
		t.Errorf("title = %q", out.Title)
	}
	if string(out.Avatar.Content) != "PNG" || out.Avatar.Filename != "a.png" {
		t.Errorf("avatar = %+v", out.Avatar)
	}
	if len(out.Docs) != 2 || string(out.Docs[1].Content) != "two" {
		t.Errorf("docs = %+v", out.Docs)
	}
}

func TestMarshalAutoDetectMultipart(t *testing.T) {
	_, ct, err := Marshal(unifiedReq{Name: "x"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Errorf("expected urlencoded, got %q", ct)
	}

	_, ct2, err := Marshal(unifiedUpload{Avatar: File{Content: []byte("x")}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if ct2 == "application/x-www-form-urlencoded" {
		t.Errorf("expected multipart, got %q", ct2)
	}
}
