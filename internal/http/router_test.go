package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginPreflightIsNotRejected(t *testing.T) {
	ts := httptest.NewServer((&Server{Timeout: time.Second}).Router())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/auth/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status %d", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("origin header %q", res.Header.Get("Access-Control-Allow-Origin"))
	}

	post, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	post.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusMethodNotAllowed {
		t.Fatal("POST login ditolak")
	}
}
