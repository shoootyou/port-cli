package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/modules/skills/catalog"
)

func TestDeleteSkill_Success_ReturnsNilError(t *testing.T) {
	var capturedMethod, capturedPath string
	var requestCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		requestCount++
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	err := catalog.DeleteSkill(context.Background(), client, "my-skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 request, got %d", requestCount)
	}
	if capturedMethod != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", capturedMethod)
	}
	if capturedPath != "/blueprints/skill/entities/my-skill" {
		t.Fatalf("expected /blueprints/skill/entities/my-skill (no /v1), got %s", capturedPath)
	}
}

func TestDeleteSkill_NotFound_ReturnsNonNilError(t *testing.T) {
	// Delete is NOT idempotent (decision D3): a 404 must return a non-nil error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "not_found",
		})
	}))
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	err := catalog.DeleteSkill(context.Background(), client, "ghost-skill")
	if err == nil {
		t.Fatal("expected non-nil error for 404 delete (delete is not idempotent), got nil")
	}
}
