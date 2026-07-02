package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckHealthAcceptsOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := checkHealth(ctx, server.URL); err != nil {
		t.Fatalf("checkHealth returned error: %v", err)
	}
}

func TestCheckHealthRejectsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := checkHealth(ctx, server.URL)
	if err == nil {
		t.Fatal("checkHealth accepted a non-OK status")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckHealthRejectsRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/healthz", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := checkHealth(ctx, server.URL+"/redirect"); err == nil {
		t.Fatal("checkHealth followed a redirect")
	}
}
