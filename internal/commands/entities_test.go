/**
 * @spec-handoff
 *
 * @interface RegisterEntities(root *cobra.Command)
 * @behavior
 *   - Registers a command group `entities` under root with subcommands: list, get, create, update, delete
 *   - All subcommands require `-b/--blueprint <id>` flag (string, required)
 *   - list: has `--output` flag (string, default "table", choices: table|json|yaml)
 *   - get: accepts one positional arg <id> (entity identifier), has `--output` flag (default "json", choices: json|yaml)
 *   - create: requires `--file <path>` flag, accepts optional `--patch` (bool), `--force` (bool)
 *   - update: accepts one positional arg <id>, requires `--file <path>` flag, accepts optional `--force` (bool)
 *   - delete: accepts one positional arg <id>, accepts optional `--force` (bool)
 *
 * @auto-detect-create-decision-table
 *   The `create` command implements auto-detect upsert logic based on entity existence:
 *   | Pre-state (GET /{blueprint}/entities/{id}) | --patch flag | API call | query params |
 *   |-------------------------------------------|--------------|----------|--------------|
 *   | 404 (not found)                            | any          | POST /blueprints/{bp}/entities | upsert=false |
 *   | 200 (exists)                               | false        | POST /blueprints/{bp}/entities | upsert=true&merge=false |
 *   | 200 (exists)                               | true         | POST /blueprints/{bp}/entities | upsert=true&merge=true |
 *
 *   When entity exists and `--force` is NOT set:
 *     - Prompt user with confirmAction("Entity <id> exists. Overwrite?", force, stdin)
 *     - If declined → abort with exit 1, NO POST call made
 *     - If confirmed → proceed with POST per table
 *   When `--force` is set → skip prompt, proceed directly
 *
 * @behavior list
 *   - GET /blueprints/{blueprint}/entities (NO /v1 prefix — base URL includes it)
 *   - Output formats:
 *     - table: columns IDENTIFIER, TITLE, TEAM; empty result → print "No entities found in blueprint <bp>", exit 0
 *     - json: raw JSON array
 *     - yaml: raw YAML array
 *
 * @behavior get
 *   - GET /blueprints/{blueprint}/entities/{id} (NO /v1 prefix)
 *   - Pre-flight: validate identifier via validateEntityIdentifier(id); reject if contains '/'
 *   - 404 → error, exit 1
 *   - Output formats: json (default), yaml
 *
 * @behavior create
 *   - Parse file via parseEntityFile(file); reject symlinks, >1MB, unsupported formats
 *   - Extract identifier from entity["identifier"]
 *   - Pre-flight: validate identifier via validateEntityIdentifier; reject if contains '/'
 *   - Warn on unknown fields via detectUnknownEntityFields → print to stderr
 *   - Existence probe: GET /blueprints/{blueprint}/entities/{identifier}
 *   - Decision tree per table above
 *   - Confirmation: if entity exists and !force → confirmAction, abort on decline
 *   - POST to /blueprints/{blueprint}/entities with query params per table
 *   - Success messages:
 *     - 404 pre-state → "Created entity <id>"
 *     - exists + !patch → "Replaced entity <id>"
 *     - exists + patch → "Merged entity <id>"
 *
 * @behavior update
 *   - Requires positional arg <id> AND --file <path>
 *   - Parse file via parseEntityFile
 *   - Extract file's identifier from entity["identifier"]
 *   - Pre-flight: if file identifier != arg identifier → error "Identifier mismatch: file has '<file_id>', expected '<arg_id>'", NO API call
 *   - Pre-flight: validate arg identifier via validateEntityIdentifier
 *   - PATCH /blueprints/{blueprint}/entities/{id} with entity body (NO /v1 prefix)
 *   - --force flag: if set, skip confirmation (future-proofing; PATCH is direct, no probe)
 *
 * @behavior delete
 *   - Requires positional arg <id>
 *   - Pre-flight: validate identifier via validateEntityIdentifier
 *   - If !force → confirmAction("Delete entity <id>?", force, stdin); abort on decline
 *   - DELETE /blueprints/{blueprint}/entities/{id} (NO /v1 prefix)
 *
 * @client-injection-seam
 *   Commands must support test-time client injection. Chosen approach:
 *   - Package-level var `testClient *api.Client` (nil in production)
 *   - Command RunE logic:
 *     1. If testClient != nil → use it
 *     2. Else → call getOrRefreshCommandToken + api.NewClient as usual
 *   - Tests set testClient in t.Cleanup to restore nil
 *   This mirrors the pattern in internal/modules/import_module/import_test.go
 *
 * @confirmation-seam
 *   - Use confirmAction(prompt, force, stdin) helper
 *   - Tests pass strings.NewReader("y\n") or strings.NewReader("n\n") as stdin
 *   - Production code passes nil → falls back to TUI
 *
 * @edge-cases
 *   - Blueprint flag missing → cobra will error before RunE
 *   - File path invalid → parseEntityFile returns error
 *   - Identifier with '/' → validateEntityIdentifier fails before API call
 *   - Confirmation declined → no API write call, exit 1
 *   - Empty entity list → friendly message, not an error
 *   - Malformed API response → decode error
 *
 * @see internal/commands/helpers.go (validateEntityIdentifier, parseEntityFile, detectUnknownEntityFields, confirmAction)
 * @see internal/api/requests.go (GetEntities, GetEntity, CreateEntityWithParams, PatchEntity, DeleteEntity)
 */

package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/spf13/cobra"
)

// ====================================================================
// Test harness: fake server + client injection
// ====================================================================

func newTestServerForEntities(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *api.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := api.NewClient(api.ClientOpts{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		APIURL:       srv.URL,
		Timeout:      0,
	})
	return srv, client
}

func authHandlerForEntities(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/auth/access_token" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          true,
			"accessToken": "fake-token",
			"expiresIn":   3600,
		})
		return true
	}
	return false
}

// ====================================================================
// list command tests
// ====================================================================

func TestEntitiesListNonEmpty(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entities": []map[string]interface{}{
					{"identifier": "svc-1", "title": "Service 1", "team": "backend"},
					{"identifier": "svc-2", "title": "Service 2", "team": "frontend"},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "list", "-b", "service", "--output", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	entities, ok := result["entities"].([]interface{})
	if !ok || len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %v", result)
	}
}

func TestEntitiesListEmpty(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entities": []interface{}{},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "list", "-b", "service", "--output", "table"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "No entities found in blueprint service") {
		t.Errorf("expected friendly empty message, got: %s", output)
	}
}

// ====================================================================
// get command tests
// ====================================================================

func TestEntitiesGetFound(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/svc-1" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "svc-1",
					"title":      "Service 1",
					"team":       "backend",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "get", "svc-1", "-b", "service", "--output", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	entity, ok := result["entity"].(map[string]interface{})
	if !ok || entity["identifier"] != "svc-1" {
		t.Fatalf("expected entity svc-1, got %v", result)
	}
}

func TestEntitiesGetNotFound(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/missing" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "get", "missing", "-b", "service"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestEntitiesGetIdentifierWithSlash(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for invalid identifier")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "get", "id/with/slash", "-b", "service"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for identifier with slash, got nil")
	}
}

// ====================================================================
// create command tests
// ====================================================================

func TestEntitiesCreateWhenNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "new-svc", "title": "New Service"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		// Existence probe
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/new-svc" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Create call
		if r.Method == "POST" && r.URL.Path == "/blueprints/service/entities" {
			query := r.URL.Query()
			if query.Get("upsert") != "false" {
				t.Errorf("expected upsert=false, got %s", query.Get("upsert"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "new-svc",
					"title":      "New Service",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Created entity new-svc") {
		t.Errorf("expected 'Created entity new-svc' message, got: %s", output)
	}
}

func TestEntitiesCreateWhenExistsReplaceWithForce(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "existing-svc", "title": "Updated Service"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		// Existence probe → exists
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/existing-svc" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "existing-svc",
					"title":      "Old Title",
				},
			})
			return
		}
		// Upsert call (no --patch → merge=false)
		if r.Method == "POST" && r.URL.Path == "/blueprints/service/entities" {
			query := r.URL.Query()
			if query.Get("upsert") != "true" {
				t.Errorf("expected upsert=true, got %s", query.Get("upsert"))
			}
			if query.Get("merge") != "false" {
				t.Errorf("expected merge=false, got %s", query.Get("merge"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "existing-svc",
					"title":      "Updated Service",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Replaced entity existing-svc") {
		t.Errorf("expected 'Replaced entity existing-svc' message, got: %s", output)
	}
}

func TestEntitiesCreateWhenExistsPatchWithForce(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "existing-svc", "title": "Merged Service"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		// Existence probe → exists
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/existing-svc" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "existing-svc",
					"title":      "Old Title",
				},
			})
			return
		}
		// Upsert call with --patch → merge=true
		if r.Method == "POST" && r.URL.Path == "/blueprints/service/entities" {
			query := r.URL.Query()
			if query.Get("upsert") != "true" {
				t.Errorf("expected upsert=true, got %s", query.Get("upsert"))
			}
			if query.Get("merge") != "true" {
				t.Errorf("expected merge=true, got %s", query.Get("merge"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "existing-svc",
					"title":      "Merged Service",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile, "--force", "--patch"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Merged entity existing-svc") {
		t.Errorf("expected 'Merged entity existing-svc' message, got: %s", output)
	}
}

func TestEntitiesCreateConfirmationDeclined(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "existing-svc", "title": "Updated Service"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	postCalled := false
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		// Existence probe → exists
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/existing-svc" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "existing-svc",
					"title":      "Old Title",
				},
			})
			return
		}
		// POST should NOT be called if confirmation declined
		if r.Method == "POST" && r.URL.Path == "/blueprints/service/entities" {
			postCalled = true
			t.Error("POST called despite confirmation declined")
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	// Inject stdin with "n" to decline confirmation
	rootCmd.SetIn(strings.NewReader("n\n"))
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when confirmation declined, got nil")
	}
	if postCalled {
		t.Fatal("POST should not have been called after declined confirmation")
	}
}

func TestEntitiesCreateIdentifierWithSlash(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "id/with/slash", "title": "Invalid"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for invalid identifier")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for identifier with slash, got nil")
	}
}

func TestEntitiesCreateFileTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "large.json")
	// Create a file > 1MB
	content := `{"identifier":"test","data":"` + strings.Repeat("x", 1024*1024+100) + `"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for oversized file")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for file too large, got nil")
	}
}

// ====================================================================
// Blueprint identifier validation tests (RED — validation not yet implemented)
// ====================================================================

func TestEntitiesGetBlueprintWithSlash(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for invalid blueprint identifier")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "get", "valid-id", "-b", "../admin"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for blueprint identifier with slash, got nil")
	}
}

func TestEntitiesCreateBlueprintWithSlash(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "valid-id", "title": "Valid"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for invalid blueprint identifier")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "create", "-b", "team/foo", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for blueprint identifier with slash, got nil")
	}
}

func TestEntitiesUpdateBlueprintWithSlash(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "svc-1", "title": "Updated"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for invalid blueprint identifier")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "update", "svc-1", "-b", "../admin", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for blueprint identifier with slash, got nil")
	}
}

func TestEntitiesDeleteBlueprintWithSlash(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for invalid blueprint identifier")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "delete", "svc-1", "-b", "team/foo", "--force"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for blueprint identifier with slash, got nil")
	}
}

// ====================================================================
// Confirmation ACCEPTED path (should PASS now)
// ====================================================================

func TestEntitiesCreateConfirmationAccepted(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "existing-svc", "title": "Updated Service"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	postCalled := false
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		// Existence probe → exists
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/existing-svc" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "existing-svc",
					"title":      "Old Title",
				},
			})
			return
		}
		// POST should be called if confirmation accepted
		if r.Method == "POST" && r.URL.Path == "/blueprints/service/entities" {
			postCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "existing-svc",
					"title":      "Updated Service",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	// Inject stdin with "y" to accept confirmation
	rootCmd.SetIn(strings.NewReader("y\n"))
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected no error when confirmation accepted, got %v", err)
	}
	if !postCalled {
		t.Fatal("POST should have been called after accepted confirmation")
	}
	output := stdout.String()
	if !strings.Contains(output, "Replaced entity existing-svc") {
		t.Errorf("expected 'Replaced entity existing-svc' message, got: %s", output)
	}
}

// ====================================================================
// update command tests
// ====================================================================

func TestEntitiesUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "svc-1", "title": "Updated Title"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	patchReceived := false
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "PATCH" && r.URL.Path == "/blueprints/service/entities/svc-1" {
			patchReceived = true
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode PATCH body: %v", err)
			}
			if body["title"] != "Updated Title" {
				t.Errorf("expected title 'Updated Title', got %v", body["title"])
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": body,
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "update", "svc-1", "-b", "service", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !patchReceived {
		t.Fatal("PATCH call was not made")
	}
}

func TestEntitiesUpdateIdentifierMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "svc-2", "title": "Wrong ID"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		t.Fatal("should not reach API call for identifier mismatch")
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "update", "svc-1", "-b", "service", "--file", entityFile, "--force"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for identifier mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") && !strings.Contains(err.Error(), "expected") {
		t.Errorf("expected mismatch error, got: %v", err)
	}
}

// ====================================================================
// delete command tests
// ====================================================================

func TestEntitiesDeleteWithForce(t *testing.T) {
	deleteReceived := false
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "DELETE" && r.URL.Path == "/blueprints/service/entities/svc-1" {
			deleteReceived = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "delete", "svc-1", "-b", "service", "--force"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !deleteReceived {
		t.Fatal("DELETE call was not made")
	}
}

func TestEntitiesDeleteConfirmationDeclined(t *testing.T) {
	deleteCalled := false
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "DELETE" && r.URL.Path == "/blueprints/service/entities/svc-1" {
			deleteCalled = true
			t.Error("DELETE called despite confirmation declined")
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetIn(strings.NewReader("n\n"))
	rootCmd.SetArgs([]string{"entities", "delete", "svc-1", "-b", "service"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when confirmation declined, got nil")
	}
	if deleteCalled {
		t.Fatal("DELETE should not have been called after declined confirmation")
	}
}

// ====================================================================
// Client injection seam validation
// ====================================================================

func TestClientInjectionSeamExists(t *testing.T) {
	// This test verifies that the package-level testClient variable exists
	// and can be used to inject a test client.
	if testClient != nil {
		t.Fatal("testClient should be nil at test start")
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	if testClient == nil {
		t.Fatal("testClient injection failed")
	}
}

// ====================================================================
// Output format validation
// ====================================================================

func TestEntitiesGetYAMLOutput(t *testing.T) {
	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/svc-1" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{
					"identifier": "svc-1",
					"title":      "Service 1",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "get", "svc-1", "-b", "service", "--output", "yaml"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "identifier") || !strings.Contains(output, "svc-1") {
		t.Errorf("expected YAML output with identifier svc-1, got: %s", output)
	}
}

// ====================================================================
// Edge case: URL query parameter construction
// ====================================================================

func TestEntitiesCreateQueryParamsFormatting(t *testing.T) {
	tmpDir := t.TempDir()
	entityFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "new-svc", "title": "New Service"}`
	if err := os.WriteFile(entityFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, client := newTestServerForEntities(t, func(w http.ResponseWriter, r *http.Request) {
		if authHandlerForEntities(w, r) {
			return
		}
		// Existence probe → not found
		if r.Method == "GET" && r.URL.Path == "/blueprints/service/entities/new-svc" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Create call → verify query params are properly formatted
		if r.Method == "POST" && r.URL.Path == "/blueprints/service/entities" {
			rawQuery := r.URL.RawQuery
			parsed, err := url.ParseQuery(rawQuery)
			if err != nil {
				t.Fatalf("failed to parse query: %v", err)
			}
			if parsed.Get("upsert") != "false" {
				t.Errorf("expected upsert=false, got %s", parsed.Get("upsert"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entity": map[string]interface{}{"identifier": "new-svc"},
			})
			return
		}
		http.NotFound(w, r)
	})

	testClient = client
	defer func() { testClient = nil }()

	rootCmd := &cobra.Command{Use: "port"}
	RegisterEntities(rootCmd)

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"entities", "create", "-b", "service", "--file", entityFile, "--force"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
