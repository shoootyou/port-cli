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

func TestGetSkill_ReturnsEntity(t *testing.T) {
	var capturedPath string
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
		capturedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{
				"identifier": "my-skill",
				"title":      "My Skill",
				"properties": map[string]interface{}{
					"description":  "Does things",
					"location":     "global",
					"instructions": "Do the thing.",
				},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	got, err := catalog.GetSkill(context.Background(), client, "my-skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 request, got %d", requestCount)
	}
	if capturedPath != "/blueprints/skill/entities/my-skill" {
		t.Fatalf("expected /blueprints/skill/entities/my-skill (no /v1), got %s", capturedPath)
	}
	if got.Identifier != "my-skill" {
		t.Errorf("Identifier: got %q, want %q", got.Identifier, "my-skill")
	}
	if got.Title != "My Skill" {
		t.Errorf("Title: got %q, want %q", got.Title, "My Skill")
	}
	if got.Location != "global" {
		t.Errorf("Location: got %q, want %q", got.Location, "global")
	}
	if got.Instructions != "Do the thing." {
		t.Errorf("Instructions: got %q, want %q", got.Instructions, "Do the thing.")
	}
}

func TestGetSkill_NotFound_ReturnsError(t *testing.T) {
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

	_, err := catalog.GetSkill(context.Background(), client, "ghost-skill")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}
