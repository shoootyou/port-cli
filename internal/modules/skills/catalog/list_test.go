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

func TestListSkills_ReturnsTwoEntities(t *testing.T) {
	var capturedPath string
	var requestCount int

	srv := newListServer(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		capturedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entities": []map[string]interface{}{
				{
					"identifier": "skill-a",
					"title":      "Skill A",
					"properties": map[string]interface{}{
						"description":  "First skill",
						"location":     "global",
						"instructions": "Do A.",
					},
				},
				{
					"identifier": "skill-b",
					"title":      "Skill B",
					"properties": map[string]interface{}{
						"description":  "Second skill",
						"location":     "project",
						"instructions": "Do B.",
					},
				},
			},
		})
	})
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	skills, err := catalog.ListSkills(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 request, got %d", requestCount)
	}
	if capturedPath != "/blueprints/skill/entities" {
		t.Fatalf("expected /blueprints/skill/entities (no /v1), got %s", capturedPath)
	}
	if skills[0].Identifier != "skill-a" {
		t.Errorf("skills[0].Identifier: got %q, want %q", skills[0].Identifier, "skill-a")
	}
	if skills[1].Identifier != "skill-b" {
		t.Errorf("skills[1].Identifier: got %q, want %q", skills[1].Identifier, "skill-b")
	}
	if skills[1].Location != "project" {
		t.Errorf("skills[1].Location: got %q, want %q", skills[1].Location, "project")
	}
}

func TestListSkills_EmptyResponse_ReturnsEmptySliceNilErr(t *testing.T) {
	srv := newListServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entities": []map[string]interface{}{},
		})
	})
	defer srv.Close()

	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})

	skills, err := catalog.ListSkills(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty slice, got %d skills", len(skills))
	}
}

// newListServer builds a test server for list tests, wiring the auth stub.
func newListServer(t *testing.T, fn http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		fn(w, r)
	}))
}
