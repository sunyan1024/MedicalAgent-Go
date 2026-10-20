package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeTransport struct{ calls, status int }

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls++
	return &http.Response{StatusCode: f.status, Header: http.Header{"X-Request-Id": []string{"synthetic-id"}}, Body: io.NopCloser(strings.NewReader(`{"id":"response-id","usage":{"prompt_tokens":12,"prompt_tokens_details":{"cached_tokens":4}}}`))}, nil
}
func request(t *testing.T) *http.Request {
	t.Helper()
	r, e := http.NewRequest("POST", "https://synthetic.invalid/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"synthetic"}],"tools":[]}`))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer NEVER_SAVE_THIS")
	return r
}
func TestCaptureWireAndBudget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COMPARE_OUT", dir)
	base := &fakeTransport{status: 200}
	c := &capture{base: base}
	for i := 0; i < 40; i++ {
		res, e := c.RoundTrip(request(t))
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(res.Body)
		if e != nil || !strings.Contains(string(b), "response-id") {
			t.Fatal("response changed", e)
		}
		res.Body.Close()
	}
	if _, e := c.RoundTrip(request(t)); e == nil {
		t.Fatal("41st model HTTP request allowed")
	}
	if base.calls != 40 {
		t.Fatal(base.calls)
	}
	files, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		b, e := os.ReadFile(filepath.Join(dir, f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "NEVER_SAVE_THIS") {
			t.Fatal("authorization leaked")
		}
	}
	b, e := os.ReadFile(filepath.Join(dir, "model-001-meta.json"))
	if e != nil || !strings.Contains(string(b), "cached_tokens") || !strings.Contains(string(b), "synthetic-id") {
		t.Fatal("usage/request ID missing", e)
	}
}
func TestCaptureStopsAfterThreeProviderErrors(t *testing.T) {
	t.Setenv("COMPARE_OUT", t.TempDir())
	base := &fakeTransport{status: 503}
	c := &capture{base: base}
	for i := 0; i < 3; i++ {
		res, e := c.RoundTrip(request(t))
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
	}
	if _, e := c.RoundTrip(request(t)); e == nil {
		t.Fatal("fourth attempt allowed after three provider errors")
	}
	if base.calls != 3 {
		t.Fatal(base.calls)
	}
}
