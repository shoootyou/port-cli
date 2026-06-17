package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/modules/skills/catalog"
)

// newTestServer builds an httptest.Server whose handler is fn, pre-wired with
// the /auth/access_token stub required by the api.Client token machinery.
// requestCount is atomically incremented for every non-auth request.
func newTestServer(t *testing.T, requestCount *atomic.Int64, fn http.HandlerFunc) (*httptest.Server, *api.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          true,
				"accessToken": "tok",
				"expiresIn":   3600,
			})
			return
		}
		if requestCount != nil {
			requestCount.Add(1)
		}
		fn(w, r)
	}))
	client := api.NewClient(api.ClientOpts{
		ClientID:     "id",
		ClientSecret: "secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})
	return srv, client
}

// ---------------------------------------------------------------------------
// Default upsert: Force=false, Patch=false
// ---------------------------------------------------------------------------

func TestCreateSkill_DefaultUpsert_POSTWithMergeTrue(t *testing.T) {
	var capturedMethod, capturedPath, capturedQuery string
	var count atomic.Int64

	srv, client := newTestServer(t, &count, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "my-skill"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:   "my-skill",
		Title:        "My Skill",
		Description:  "Does things",
		Location:     "global",
		Instructions: "Do the thing.",
	}
	err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("expected exactly 1 request, got %d", count.Load())
	}
	if capturedMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/blueprints/skill/entities" {
		t.Fatalf("expected /blueprints/skill/entities (no /v1), got %s", capturedPath)
	}
	q, err := url.ParseQuery(capturedQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	if q.Get("upsert") != "true" {
		t.Errorf("expected upsert=true, got %q", q.Get("upsert"))
	}
	if q.Get("merge") != "true" {
		t.Errorf("expected merge=true (default upsert), got %q", q.Get("merge"))
	}
}

// ---------------------------------------------------------------------------
// Force=true → merge=false
// ---------------------------------------------------------------------------

func TestCreateSkill_Force_POSTWithMergeFalse(t *testing.T) {
	var capturedQuery string
	var count atomic.Int64

	srv, client := newTestServer(t, &count, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "force-skill"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "force-skill",
		Description: "Force upsert",
		Location:    "global",
	}
	err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{Force: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("expected exactly 1 request, got %d", count.Load())
	}
	q, err := url.ParseQuery(capturedQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	if q.Get("merge") != "false" {
		t.Errorf("expected merge=false for Force=true, got %q", q.Get("merge"))
	}
}

// ---------------------------------------------------------------------------
// Patch=true → PATCH /blueprints/skill/entities/<identifier>
// ---------------------------------------------------------------------------

func TestCreateSkill_Patch_PATCHWithIdentifierInPath(t *testing.T) {
	var capturedMethod, capturedPath string
	var count atomic.Int64

	srv, client := newTestServer(t, &count, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "patch-skill"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "patch-skill",
		Description: "Patched",
		Location:    "global",
	}
	err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{Patch: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("expected exactly 1 request, got %d", count.Load())
	}
	if capturedMethod != http.MethodPatch {
		t.Fatalf("expected PATCH, got %s", capturedMethod)
	}
	if capturedPath != "/blueprints/skill/entities/patch-skill" {
		t.Fatalf("expected /blueprints/skill/entities/patch-skill, got %s", capturedPath)
	}
}

// ---------------------------------------------------------------------------
// Force=true AND Patch=true → error BEFORE any HTTP call
// ---------------------------------------------------------------------------

func TestCreateSkill_ForceAndPatch_ErrorBeforeHTTP(t *testing.T) {
	var count atomic.Int64

	srv, client := newTestServer(t, &count, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "should-not-reach"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "conflict-skill",
		Description: "Should fail",
		Location:    "global",
	}
	err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{Force: true, Patch: true})
	if err == nil {
		t.Fatal("expected error for Force=true and Patch=true, got nil")
	}
	// Zero requests must hit the server — validation fires before any HTTP call.
	if count.Load() != 0 {
		t.Fatalf("expected zero HTTP requests, got %d", count.Load())
	}
}

// ---------------------------------------------------------------------------
// Blueprint missing → error mentions catalog blueprint init hint
// ---------------------------------------------------------------------------

func TestCreateSkill_BlueprintMissing_ErrorMentionsBlueprintInit(t *testing.T) {
	srv, client := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"error":   "not_found",
			"message": "Blueprint skill not found",
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "missing-bp-skill",
		Description: "Blueprint missing",
		Location:    "global",
	}
	err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{})
	if err == nil {
		t.Fatal("expected error when blueprint missing, got nil")
	}
	if !strings.Contains(err.Error(), "port skills catalog blueprint init") {
		t.Errorf("expected error to contain 'port skills catalog blueprint init', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HTTP 500 → error propagated, Port body surfaced
// ---------------------------------------------------------------------------

func TestCreateSkill_HTTP500_ErrorPropagated(t *testing.T) {
	srv, client := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"error":   "internal_error",
			"message": "something went wrong",
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "error-skill",
		Description: "Should fail with 500",
		Location:    "global",
	}
	err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{})
	if err == nil {
		t.Fatal("expected error from HTTP 500, got nil")
	}
	// The error message should surface something about the failure (status or body).
	errStr := err.Error()
	if !strings.Contains(errStr, "500") && !strings.Contains(errStr, "Internal Server Error") && !strings.Contains(errStr, "internal_error") {
		t.Errorf("expected error to surface HTTP body/status, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HTTP 200 and 201 → success, nil error
// ---------------------------------------------------------------------------

func TestCreateSkill_HTTP200_Success(t *testing.T) {
	srv, client := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "ok-skill"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "ok-skill",
		Description: "Success",
		Location:    "global",
	}
	if err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{}); err != nil {
		t.Fatalf("expected nil error on HTTP 200, got: %v", err)
	}
}

func TestCreateSkill_HTTP201_Success(t *testing.T) {
	srv, client := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "created-skill"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "created-skill",
		Description: "Created",
		Location:    "global",
	}
	if err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{}); err != nil {
		t.Fatalf("expected nil error on HTTP 201, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unicode wire check — accents/⚠️/→/— round-trip through JSON losslessly
// ---------------------------------------------------------------------------

func TestCreateSkill_UnicodeWireRoundTrip(t *testing.T) {
	// Instructions containing Spanish accents, emoji, arrow, em-dash — identical
	// to real skill file content.
	unicodeInstructions := "La SBS exige retención por 7 años mínimo.\n\n> ⚠️ locked = true no se puede revertir.\n\nPatrón → seguir las reglas — siempre."

	var decodedInstructions string

	srv, client := newTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		// Decode the JSON request body and extract properties.instructions.
		var body struct {
			Properties struct {
				Instructions string `json:"instructions"`
			} `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		decodedInstructions = body.Properties.Instructions
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "unicode-skill"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:   "unicode-skill",
		Title:        "Unicode Skill",
		Description:  "Unicode wire test",
		Location:     "global",
		Instructions: unicodeInstructions,
	}
	if err := catalog.CreateSkill(context.Background(), client, entity, catalog.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// JSON encoding may \u-escape < > & but decoding back must be string-equal.
	if decodedInstructions != unicodeInstructions {
		t.Errorf("unicode round-trip failed:\ngot:  %q\nwant: %q", decodedInstructions, unicodeInstructions)
	}
}
