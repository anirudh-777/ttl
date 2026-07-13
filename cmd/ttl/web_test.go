package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebHandlerServesPWAAssets(t *testing.T) {
	h := webHandler()

	t.Run("manifest", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var manifest struct {
			StartURL string `json:"start_url"`
			Display  string `json:"display"`
			Icons    []any  `json:"icons"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		if manifest.StartURL != "/today" || manifest.Display != "standalone" || len(manifest.Icons) < 2 {
			t.Fatalf("unexpected manifest: %+v", manifest)
		}
	})

	t.Run("service worker", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Service-Worker-Allowed"); got != "/" {
			t.Fatalf("Service-Worker-Allowed = %q, want /", got)
		}
		body := w.Body.String()
		if !strings.Contains(body, "url.pathname.startsWith('/api/')") {
			t.Fatal("service worker must explicitly bypass API requests")
		}
	})
}
