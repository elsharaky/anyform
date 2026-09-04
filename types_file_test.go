package anyform

import (
	"bytes"
	"mime/multipart"
	"net"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// --- Custom converters ---

type Currency int

const (
	USD Currency = iota
	EUR
)

type currencyConverter struct{}

func (currencyConverter) Marshal(v reflect.Value) (string, error) {
	switch v.Int() {
	case 0:
		return "USD", nil
	case 1:
		return "EUR", nil
	}
	return "UNKNOWN", nil
}

func (currencyConverter) Unmarshal(s string, f reflect.Value) error {
	switch strings.ToUpper(s) {
	case "USD":
		f.SetInt(0)
	case "EUR":
		f.SetInt(1)
	}
	return nil
}

type convertStruct struct {
	Price  Currency `form:"price"`
	Amount Currency `form:"amount"`
}

func TestCustomConverter(t *testing.T) {
	enc := NewEncoder(WithCustomConverter(reflect.TypeOf(Currency(0)), currencyConverter{}))
	in := convertStruct{Price: USD, Amount: EUR}
	vals, err := enc.Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("price") != "USD" || vals.Get("amount") != "EUR" {
		t.Errorf("vals = %v", vals)
	}

	dec := NewDecoder(WithCustomConverter(reflect.TypeOf(Currency(0)), currencyConverter{}))
	var out convertStruct
	if err := dec.Unmarshal(url.Values{"price": {"EUR"}, "amount": {"USD"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Price != EUR || out.Amount != USD {
		t.Errorf("out = %+v", out)
	}
}

func TestDurationConverter(t *testing.T) {
	// time.Duration implements fmt.Stringer but not TextMarshaler; test via parse
	enc := NewEncoder()

	type s struct {
		D time.Duration `form:"d"`
	}
	vals, err := enc.Marshal(s{D: 90 * time.Second})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("d") != "1m30s" {
		t.Errorf("d = %q", vals.Get("d"))
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"d": {"1m30s"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.D != 90*time.Second {
		t.Errorf("D = %v", out.D)
	}
}

func TestIPConverter(t *testing.T) {
	enc := NewEncoder()

	type s struct {
		IP net.IP `form:"ip"`
	}
	vals, err := enc.Marshal(s{IP: net.ParseIP("192.168.1.10")})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("ip") != "192.168.1.10" {
		t.Errorf("ip = %q", vals.Get("ip"))
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"ip": {"10.0.0.1"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !out.IP.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("IP = %v", out.IP)
	}

	// empty value unmarshals to nil IP (not an error)
	var empty s
	if err := dec.Unmarshal(url.Values{"ip": {""}}, &empty); err != nil {
		t.Fatalf("unmarshal empty error: %v", err)
	}
	if empty.IP != nil {
		t.Errorf("IP = %v, want nil", empty.IP)
	}

	// invalid IP reports a parse error
	if err := dec.Unmarshal(url.Values{"ip": {"not-an-ip"}}, &s{}); err == nil {
		t.Error("expected error for invalid IP")
	}
}

func TestURLConverter(t *testing.T) {
	enc := NewEncoder()

	type s struct {
		Link url.URL `form:"link"`
	}
	vals, err := enc.Marshal(s{Link: url.URL{Scheme: "https", Host: "example.com", Path: "/x"}})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("link") != "https://example.com/x" {
		t.Errorf("link = %q", vals.Get("link"))
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"link": {"https://example.com/x?q=1"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Link.String() != "https://example.com/x?q=1" {
		t.Errorf("Link = %v", out.Link)
	}

	// empty value is left as-is (zero url.URL)
	var empty s
	if err := dec.Unmarshal(url.Values{"link": {""}}, &empty); err != nil {
		t.Fatalf("unmarshal empty error: %v", err)
	}
	if empty.Link.String() != "" {
		t.Errorf("Link = %v, want zero", empty.Link.String())
	}

	// invalid URL reports a parse error
	if err := dec.Unmarshal(url.Values{"link": {"://%"}}, &s{}); err == nil {
		t.Error("expected error for invalid URL")
	}
}

// --- File / multipart ---

type uploadStruct struct {
	Title  string `form:"title"`
	Avatar File   `form:"avatar"`
	Docs   []File `form:"documents"`
}

func TestMarshalMultipart_Files(t *testing.T) {
	enc := NewEncoder()
	in := uploadStruct{
		Title:  "hello",
		Avatar: File{Content: []byte("png-data"), ContentType: "image/png", Filename: "a.png"},
		Docs: []File{
			{Content: []byte("doc1"), ContentType: "text/plain", Filename: "d1.txt"},
			{Content: []byte("doc2"), ContentType: "text/plain", Filename: "d2.txt"},
		},
	}

	body, contentType, err := enc.MarshalMultipart(in)
	if err != nil {
		t.Fatalf("marshal multipart error: %v", err)
	}

	// Parse back via an http request.
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse multipart: %v", err)
	}

	if req.FormValue("title") != "hello" {
		t.Errorf("title = %q", req.FormValue("title"))
	}

	var out uploadStruct
	dec := NewDecoder()
	if err := dec.UnmarshalMultipart(req, &out); err != nil {
		t.Fatalf("unmarshal multipart error: %v", err)
	}

	if out.Title != "hello" {
		t.Errorf("Title = %q", out.Title)
	}
	if string(out.Avatar.Content) != "png-data" {
		t.Errorf("Avatar.Content = %q", out.Avatar.Content)
	}
	if out.Avatar.Filename != "a.png" {
		t.Errorf("Avatar.Filename = %q", out.Avatar.Filename)
	}
	if len(out.Docs) != 2 {
		t.Fatalf("Docs len = %d", len(out.Docs))
	}
	if out.Docs[0].Filename != "d1.txt" || string(out.Docs[1].Content) != "doc2" {
		t.Errorf("Docs = %+v", out.Docs)
	}
}

func TestFileFromRequest_NoFiles(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	files, err := FilesFromRequest(req, "nonexistent")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil files, got %v", files)
	}
}

func TestFileFromHeader_DetectContentType(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("f", "x.txt")
	_, _ = fw.Write([]byte("hello file content"))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse: %v", err)
	}

	f, err := FileFromHeader(req.MultipartForm.File["f"][0])
	if err != nil {
		t.Fatalf("FileFromHeader: %v", err)
	}
	if string(f.Content) != "hello file content" {
		t.Errorf("content = %q", f.Content)
	}
	if f.ContentType == "" {
		t.Error("expected detected content type")
	}
}
