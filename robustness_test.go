package anyform

import (
	"errors"
	"sync"
	"testing"
)

func TestWithZeroEmptyOmitsZeroValues(t *testing.T) {
	type z struct {
		Name  string  `form:"name"`
		Age   int     `form:"age"`
		Score float64 `form:"score"`
	}

	// Default: zero values are emitted.
	vals, err := NewEncoder().Marshal(z{Name: "x"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if vals.Get("age") != "0" || vals.Get("score") != "0" {
		t.Errorf("default: expected zero values emitted, got %v", vals)
	}

	// WithZeroEmpty: zero values omitted, provided values kept.
	enc := NewEncoder(WithZeroEmpty(true))
	vals, err = enc.Marshal(z{Name: "x", Age: 7})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if vals.Get("age") != "7" {
		t.Errorf("age = %q, want 7", vals.Get("age"))
	}
	if _, ok := vals["score"]; ok {
		t.Errorf("expected score omitted, got %v", vals)
	}
	if vals.Get("name") != "x" {
		t.Errorf("name = %q, want x", vals.Get("name"))
	}
}

type node struct {
	Name  string `form:"name"`
	Child *node  `form:"child"`
}

func TestCircularReferenceReturnsMaxDepthError(t *testing.T) {
	n := &node{Name: "root"}
	n.Child = n // self-cycle

	enc := NewEncoder(WithMaxDepth(8))
	_, err := enc.Marshal(n)
	if err == nil {
		t.Fatal("expected error for cyclic struct")
	}
	if !errors.Is(err, ErrMaxDepthExceeded) {
		t.Errorf("expected ErrMaxDepthExceeded, got %v", err)
	}
}

func TestCircularReferenceScanForFilesDoesNotHang(t *testing.T) {
	n := &node{Name: "root"}
	n.Child = n

	body, ct, err := Marshal(n)
	if err == nil {
		t.Fatal("expected error for cyclic struct")
	}
	if !errors.Is(err, ErrMaxDepthExceeded) {
		t.Errorf("expected ErrMaxDepthExceeded, got %v", err)
	}
	_ = body
	_ = ct
}

// Regression: scanning for File fields must treat the visited set as a call
// stack, so the same struct type appearing twice via interface{} fields is
// scanned both times. Previously the second (file-bearing) instance was
// skipped and Marshal picked url-encoded, then failed.
type scanWrapper struct {
	Data any `form:"data"`
}

type scanPayload struct {
	First  scanWrapper
	Second scanWrapper
}

func TestScanForFiles_RechecksSiblingInterfaceFields(t *testing.T) {
	p := scanPayload{
		First:  scanWrapper{Data: "just a string"},
		Second: scanWrapper{Data: File{Content: []byte("x"), Filename: "f.bin"}},
	}
	body, ct, err := Marshal(p)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected a multipart body")
	}
	if ct == urlEncodedContentType {
		t.Error("expected multipart content type, got url-encoded")
	}
}

func TestEncoderDecoderThreadSafety(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()

	type u struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				vals, err := enc.Marshal(u{Name: "x", Age: i})
				if err != nil {
					t.Errorf("Marshal: %v", err)
					return
				}
				var out u
				if err := dec.Unmarshal(vals, &out); err != nil {
					t.Errorf("Unmarshal: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestTopLevelMarshalUnmarshalConcurrent(t *testing.T) {
	type u struct {
		Name string `form:"name"`
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, ct, err := Marshal(u{Name: "x"})
			if err != nil {
				t.Errorf("Marshal: %v", err)
				return
			}
			var out u
			if err := Unmarshal(body, ct, &out); err != nil {
				t.Errorf("Unmarshal: %v", err)
			}
		}()
	}
	wg.Wait()
}
