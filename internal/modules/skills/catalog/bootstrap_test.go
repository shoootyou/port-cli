package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/modules/skills/catalog"
)

// ---------------------------------------------------------------------------
// BootstrapBlueprint: GET /blueprints/skill 200 → nil, no POST issued
// ---------------------------------------------------------------------------

func TestBootstrapBlueprint_BlueprintExists_NilErrorNoPost(t *testing.T) {
	var postCount atomic.Int64
	var getCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/blueprints/skill" {
			getCount.Add(1)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"blueprint": map[string]interface{}{
					"identifier": "skill",
					"title":      "Skill",
				},
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/blueprints" {
			postCount.Add(1)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"blueprint": map[string]interface{}{"identifier": "skill"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	err := catalog.BootstrapBlueprint(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error when blueprint exists: %v", err)
	}
	if getCount.Load() != 1 {
		t.Fatalf("expected exactly 1 GET /blueprints/skill, got %d", getCount.Load())
	}
	// Must NOT POST when blueprint already exists.
	if postCount.Load() != 0 {
		t.Fatalf("expected zero POST /blueprints calls, got %d (blueprint already existed)", postCount.Load())
	}
}

// ---------------------------------------------------------------------------
// BootstrapBlueprint: GET 404 → POST /blueprints with skill schema → 201 → nil
// ---------------------------------------------------------------------------

func TestBootstrapBlueprint_BlueprintMissing_POSTsSkillSchema(t *testing.T) {
	var postCount atomic.Int64
	var capturedPostBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/blueprints/skill" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "not_found",
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/blueprints" {
			postCount.Add(1)
			if err := json.NewDecoder(r.Body).Decode(&capturedPostBody); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"blueprint": map[string]interface{}{"identifier": "skill"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	err := catalog.BootstrapBlueprint(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if postCount.Load() != 1 {
		t.Fatalf("expected exactly 1 POST /blueprints, got %d", postCount.Load())
	}
	// The POST body must carry identifier="skill".
	if capturedPostBody == nil {
		t.Fatal("expected POST body to be non-nil")
	}
	if capturedPostBody["identifier"] != "skill" {
		t.Errorf("expected POST body identifier=skill, got %v", capturedPostBody["identifier"])
	}
}

// ---------------------------------------------------------------------------
// BootstrapBlueprint: GET 404 → POST 409 (already exists) → nil (idempotent, D4)
// ---------------------------------------------------------------------------

func TestBootstrapBlueprint_PostConflict_NilError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/blueprints/skill" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "not_found",
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/blueprints" {
			// 409 Conflict: blueprint already exists (race or concurrent call).
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":      false,
				"error":   "already_exists",
				"message": "Blueprint skill already exists",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	// Decision D4: POST 409 is treated as success — bootstrap is idempotent.
	err := catalog.BootstrapBlueprint(context.Background(), client)
	if err != nil {
		t.Fatalf("expected nil error on POST 409 (idempotent bootstrap), got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// BootstrapBlueprint: GET 404 → POST 500 → error
// ---------------------------------------------------------------------------

func TestBootstrapBlueprint_PostServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/blueprints/skill" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "not_found",
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/blueprints" {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":      false,
				"error":   "internal_error",
				"message": "storage unavailable",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	err := catalog.BootstrapBlueprint(context.Background(), client)
	if err == nil {
		t.Fatal("expected error on POST 500, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "500") && !strings.Contains(errStr, "Internal Server Error") && !strings.Contains(errStr, "internal_error") {
		t.Errorf("expected error to surface HTTP body/status, got: %v", err)
	}
}
