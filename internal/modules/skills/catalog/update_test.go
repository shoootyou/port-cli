// Tests for UpdateSkill (finding F — coverage characterisation).
//
// UpdateSkill is a thin wrapper around CreateSkill{Patch:true}.
// These tests close the coverage gap by exercising the happy path and the
// 404 error path directly against UpdateSkill.  They may pass immediately
// (F is allowed to be green) but they also ensure that once finding A is
// fixed (PATCH 404 → blueprint-init hint), the UpdateSkill surface also
// benefits from the fix.

package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/port-experimental/port-cli/internal/modules/skills/catalog"
)

// ---------------------------------------------------------------------------
// F — UpdateSkill happy-path characterisation
// ---------------------------------------------------------------------------

// TestUpdateSkill_Success_PATCHesCorrectPath asserts that UpdateSkill issues
// PATCH /blueprints/skill/entities/<identifier> and returns nil on HTTP 200.
func TestUpdateSkill_Success_PATCHesCorrectPath(t *testing.T) {
	var capturedMethod, capturedPath string
	var count atomic.Int64

	srv, client := newTestServer(t, &count, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{"identifier": "update-skill"},
		})
	})
	defer srv.Close()

	entity := catalog.SkillEntity{
		Identifier:  "update-skill",
		Description: "Updated",
		Location:    "global",
	}
	err := catalog.UpdateSkill(context.Background(), client, entity)
	if err != nil {
		t.Fatalf("expected nil error from UpdateSkill on HTTP 200, got: %v", err)
	}
	if count.Load() != 1 {
		t.Fatalf("expected exactly 1 request, got %d", count.Load())
	}
	if capturedMethod != http.MethodPatch {
		t.Fatalf("expected PATCH, got %s", capturedMethod)
	}
	if capturedPath != "/blueprints/skill/entities/update-skill" {
		t.Fatalf("expected /blueprints/skill/entities/update-skill, got %s", capturedPath)
	}
}

// TestUpdateSkill_404_ErrorMentionsBlueprintInit asserts that UpdateSkill
// surfaces the blueprint-init hint when the server returns 404.
//
// This test may start RED (before fix A) and turn green alongside A — that's
// intentional: it documents that UpdateSkill inherits the fix automatically
// because it delegates to CreateSkill{Patch:true}.
func TestUpdateSkill_404_ErrorMentionsBlueprintInit(t *testing.T) {
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
		Identifier:  "update-missing-bp",
		Description: "Blueprint missing for update",
		Location:    "global",
	}
	err := catalog.UpdateSkill(context.Background(), client, entity)
	if err == nil {
		t.Fatal("expected error when blueprint missing, got nil")
	}
	if !strings.Contains(err.Error(), "port skills catalog blueprint init") {
		t.Errorf("expected error to contain 'port skills catalog blueprint init', got: %v", err)
	}
}
