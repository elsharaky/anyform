package anyform

import (
	"errors"
	"net/url"
	"testing"
)

type defaultsReq struct {
	Name   string `form:"name"`
	Region string `form:"region,default:us-east"`
	Count  int    `form:"count,default:5"`
	Token  string `form:"token,required"`
	Flag   bool   `form:"flag,default:true"`
}

func TestDefaultTagAppliedWhenMissing(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"name": {"x"}, "token": {"abc"}}
	if err := NewDecoder().Unmarshal(vals, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Region != "us-east" {
		t.Errorf("Region = %q, want us-east", r.Region)
	}
	if r.Count != 5 {
		t.Errorf("Count = %d, want 5", r.Count)
	}
	if !r.Flag {
		t.Errorf("Flag = false, want true")
	}
	if r.Name != "x" || r.Token != "abc" {
		t.Errorf("unexpected: %+v", r)
	}
}

func TestDefaultTagPreservesProvidedValue(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"name": {"x"}, "region": {"eu-west"}, "token": {"abc"}}
	if err := NewDecoder().Unmarshal(vals, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Region != "eu-west" {
		t.Errorf("Region = %q, want provided eu-west", r.Region)
	}
}

func TestRequiredMissing(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"name": {"x"}} // token missing
	err := NewDecoder().Unmarshal(vals, &r)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %T", err)
	}
	if de.Key != "token" {
		t.Errorf("DecodingError.Key = %q, want token", de.Key)
	}
}

func TestRequiredProvidedIsOK(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"token": {"abc"}}
	if err := NewDecoder().Unmarshal(vals, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Token != "abc" {
		t.Errorf("Token = %q", r.Token)
	}
}
