/**
 * @spec-handoff
 *
 * @interface RegisterWorkflows(root *cobra.Command)
 * @behavior
 *   - Registers a "workflows" parent command under root with aliases ["workflow", "wf"]
 *   - Registers 5 subcommands: list, get, create, update, delete
 *   - Uses the testClient seam pattern: `var testClient *api.Client` at package level
 *     - In production (testClient == nil): use normal auth flow
 *     - In tests: set testClient to httptest-backed client, defer reset to nil
 *   - All subcommands call client methods via testClient if set, else production client
 *
 * @subcommand list
 * @flags --output (string, default "table", choices: table|json|yaml)
 * @behavior
 *   - Calls client.GetWorkflows(ctx)
 *   - Empty list: print "No workflows found", exit 0
 *   - Non-empty: format as table (IDENTIFIER | TITLE columns) or json|yaml
 *   - Table format: header row, then one row per workflow
 *
 * @subcommand get <identifier>
 * @flags --output (string, default "json", choices: json|yaml)
 * @behavior
 *   - Pre-flight: reject if identifier contains '/' (call validateWorkflowIdentifier)
 *   - Calls client.GetWorkflow(ctx, identifier)
 *   - 404 → error message (detectable via errors.Is(err, api.ErrWorkflowNotFound))
 *   - Success: format as json|yaml
 *
 * @subcommand create --file <path>
 * @flags --file (string, required), --force (bool, default false)
 * @behavior
 *   - Parse file via parseWorkflowFile → (body, identifier, err)
 *   - Pre-flight: reject if identifier contains '/' (call validateWorkflowIdentifier)
 *   - Pre-flight: reject if file > 1MB (parseWorkflowFile does this)
 *   - GET probe: call client.GetWorkflow(ctx, identifier)
 *     - If ErrWorkflowNotFound (404): call client.CreateWorkflow(ctx, body), print "Created workflow {id}", exit 0
 *     - If workflow exists: RECREATE mode (see decision table below)
 *
 * @decision-table create (assert exact API call sequence in tests)
 * | pre-state (GET probe)        | --force | user input | API calls                          | output message           |
 * |------------------------------|---------|------------|------------------------------------|--------------------------|
 * | not found (404)              | any     | any        | POST /workflows                    | "Created workflow {id}"  |
 * | exists                       | true    | n/a        | DELETE /workflows/{id}, POST       | "Replaced workflow {id}" |
 * | exists                       | false   | confirm y  | DELETE /workflows/{id}, POST       | "Replaced workflow {id}" |
 * | exists                       | false   | decline n  | (none)                             | "Aborted"                |
 *
 * @recreate-rollback CRITICAL behavior (test: TestWorkflowsCreateRecreateRollbackOnPostFailure)
 * When recreate mode runs:
 *   1. GET /workflows/{id} → succeeds, store oldWorkflow
 *   2. DELETE /workflows/{id} → succeeds
 *   3. POST /workflows (new body) → FAILS (e.g., 500)
 *   4. ROLLBACK: POST /workflows (oldWorkflow body) → restore the deleted workflow
 *   5. Return error to user mentioning the rollback attempt
 * Test must assert:
 *   - DELETE happened (1 call)
 *   - First POST happened with new body (failed)
 *   - Rollback POST happened with old body (succeeded)
 *   - Command returns error
 *
 * @subcommand update <identifier> --file <path>
 * @flags --file (string, required), --force (bool, default false)
 * @behavior
 *   - Parse file via parseWorkflowFile → (body, fileIdentifier, err)
 *   - Pre-flight: if fileIdentifier != identifier → error "identifier mismatch", no API calls
 *   - Pre-flight: reject if identifier contains '/' (call validateWorkflowIdentifier)
 *   - RECREATE mode: DELETE then POST (same as create when exists)
 *   - Confirmation prompt unless --force
 *   - Success: print "Updated workflow {id}"
 *
 * @subcommand delete <identifier>
 * @flags --force (bool, default false)
 * @behavior
 *   - Pre-flight: reject if identifier contains '/' (call validateWorkflowIdentifier)
 *   - Confirmation prompt unless --force (uses confirmAction via cmd.InOrStdin())
 *   - If confirmed: call client.DeleteWorkflow(ctx, identifier), print "Deleted workflow {id}"
 *   - If declined: print "Aborted", no API call
 *
 * @confirmation-mechanism
 *   - Use confirmAction(prompt, force, cmd.InOrStdin())
 *   - Tests inject input via rootCmd.SetIn(strings.NewReader("y\n")) or similar
 *   - Prompt reads from cmd.InOrStdin(), not os.Stdin directly
 *
 * @testClient-seam
 *   - Package-level var testClient *api.Client
 *   - Production code: if testClient != nil { use testClient } else { use normal auth client }
 *   - Test setup: testClient = api.NewClient(...httptest server...), defer func() { testClient = nil }()
 *   - Ensures tests are independent and don't hit real API
 *
 * @see internal/commands/workflows_helpers.go for helper functions
 * @see internal/api/requests.go for client methods and ErrWorkflowNotFound
 */

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/port-experimental/port-cli/internal/api"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ====================================================================
// Test helpers
// ====================================================================

// setupTestCommand creates a root command with workflows registered and httptest server.
// Returns root command, server, and cleanup function.
func setupTestCommand(handler http.HandlerFunc) (*cobra.Command, *httptest.Server, func()) {
	server := httptest.NewServer(handler)
	client := api.NewClient(api.ClientOpts{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		APIURL:       server.URL,
		Timeout:      0,
	})
	testClient = client

	rootCmd := &cobra.Command{Use: "port"}
	RegisterWorkflows(rootCmd)

	cleanup := func() {
		server.Close()
		testClient = nil
	}

	return rootCmd, server, cleanup
}

// executeCommand executes a command with args and returns stdout, stderr, and error.
func executeCommand(rootCmd *cobra.Command, args ...string) (stdout string, stderr string, err error) {
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	rootCmd.SetOut(stdoutBuf)
	rootCmd.SetErr(stderrBuf)
	rootCmd.SetArgs(args)

	err = rootCmd.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// authHandler handles /auth/access_token requests in tests.
// Returns true if the request was handled (auth request), false otherwise.
func authHandler(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/auth/access_token" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          true,
			"accessToken": "test-token",
			"expiresIn":   3600,
		})
		return true
	}
	return false
}

// ====================================================================
// list command tests
// ====================================================================

func TestWorkflowsListNonEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflows": []map[string]interface{}{
					{"identifier": "workflow-1", "title": "Workflow One"},
					{"identifier": "workflow-2", "title": "Workflow Two"},
				},
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	stdout, _, err := executeCommand(rootCmd, "workflows", "list", "--output", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify table output contains header and rows
	if !strings.Contains(stdout, "IDENTIFIER") {
		t.Error("expected table header with IDENTIFIER column")
	}
	if !strings.Contains(stdout, "TITLE") {
		t.Error("expected table header with TITLE column")
	}
	if !strings.Contains(stdout, "workflow-1") {
		t.Error("expected workflow-1 in output")
	}
	if !strings.Contains(stdout, "Workflow One") {
		t.Error("expected 'Workflow One' in output")
	}
	if !strings.Contains(stdout, "workflow-2") {
		t.Error("expected workflow-2 in output")
	}
}

func TestWorkflowsListNonEmptyJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflows": []map[string]interface{}{
					{"identifier": "workflow-1", "title": "Workflow One"},
				},
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	stdout, _, err := executeCommand(rootCmd, "workflows", "list", "--output", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse JSON output
	var workflows []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &workflows); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if len(workflows) != 1 {
		t.Errorf("expected 1 workflow, got %d", len(workflows))
	}
	if workflows[0]["identifier"] != "workflow-1" {
		t.Errorf("expected workflow-1, got %v", workflows[0]["identifier"])
	}
}

func TestWorkflowsListEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":        true,
				"workflows": []map[string]interface{}{},
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	stdout, _, err := executeCommand(rootCmd, "workflows", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify empty message
	if !strings.Contains(stdout, "No workflows found") {
		t.Errorf("expected 'No workflows found', got: %s", stdout)
	}
}

// ====================================================================
// get command tests
// ====================================================================

func TestWorkflowsGetFoundJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflow": map[string]interface{}{
					"identifier": "test-workflow",
					"title":      "Test Workflow",
				},
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	stdout, _, err := executeCommand(rootCmd, "workflows", "get", "test-workflow", "--output", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse JSON output
	var workflow map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &workflow); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if workflow["identifier"] != "test-workflow" {
		t.Errorf("expected test-workflow, got %v", workflow["identifier"])
	}
}

func TestWorkflowsGetFoundYAML(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflow": map[string]interface{}{
					"identifier": "test-workflow",
					"title":      "Test Workflow",
				},
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	stdout, _, err := executeCommand(rootCmd, "workflows", "get", "test-workflow", "--output", "yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse YAML output
	var workflow map[string]interface{}
	if err := yaml.Unmarshal([]byte(stdout), &workflow); err != nil {
		t.Fatalf("failed to parse YAML output: %v", err)
	}
	if workflow["identifier"] != "test-workflow" {
		t.Errorf("expected test-workflow, got %v", workflow["identifier"])
	}
}

func TestWorkflowsGetNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/nonexistent" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":      false,
				"message": "Workflow not found",
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	_, _, err := executeCommand(rootCmd, "workflows", "get", "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}

	// Verify error is ErrWorkflowNotFound
	if !errors.Is(err, api.ErrWorkflowNotFound) {
		t.Errorf("expected ErrWorkflowNotFound via errors.Is, got: %v", err)
	}
}

func TestWorkflowsGetInvalidIdentifierWithSlash(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		// Should never reach API due to pre-flight validation
		t.Fatal("API should not be called for invalid identifier")
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	_, _, err := executeCommand(rootCmd, "workflows", "get", "invalid/id")
	if err == nil {
		t.Fatal("expected error for identifier with slash, got nil")
	}

	// Verify it's a validation error (ErrInvalidIdentifier)
	if !errors.Is(err, ErrInvalidIdentifier) {
		t.Errorf("expected ErrInvalidIdentifier, got: %v", err)
	}
}

// ====================================================================
// create command tests
// ====================================================================

func TestWorkflowsCreateNotExists(t *testing.T) {
	var apiCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/new-workflow" && r.Method == http.MethodGet {
			// GET probe → 404
			apiCalls = append(apiCalls, "GET /workflows/new-workflow")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false})
		} else if r.URL.Path == "/workflows" && r.Method == http.MethodPost {
			// CREATE
			apiCalls = append(apiCalls, "POST /workflows")
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":       true,
				"workflow": body,
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	// Create temp file
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"new-workflow","title":"New Workflow"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := executeCommand(rootCmd, "workflows", "create", "--file", workflowFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output
	if !strings.Contains(stdout, "Created workflow") {
		t.Errorf("expected 'Created workflow' message, got: %s", stdout)
	}
	if !strings.Contains(stdout, "new-workflow") {
		t.Errorf("expected identifier in output, got: %s", stdout)
	}

	// Verify API calls: GET probe (404), then POST
	if len(apiCalls) != 2 {
		t.Fatalf("expected 2 API calls, got %d: %v", len(apiCalls), apiCalls)
	}
	if apiCalls[0] != "GET /workflows/new-workflow" {
		t.Errorf("expected GET probe first, got: %s", apiCalls[0])
	}
	if apiCalls[1] != "POST /workflows" {
		t.Errorf("expected POST second, got: %s", apiCalls[1])
	}
}

func TestWorkflowsCreateExistsForce(t *testing.T) {
	var apiCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/existing-workflow" && r.Method == http.MethodGet {
			// GET probe → exists
			apiCalls = append(apiCalls, "GET /workflows/existing-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflow": map[string]interface{}{
					"identifier": "existing-workflow",
					"title":      "Old Title",
				},
			})
		} else if r.URL.Path == "/workflows/existing-workflow" && r.Method == http.MethodDelete {
			// DELETE
			apiCalls = append(apiCalls, "DELETE /workflows/existing-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		} else if r.URL.Path == "/workflows" && r.Method == http.MethodPost {
			// POST (recreate)
			apiCalls = append(apiCalls, "POST /workflows")
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":       true,
				"workflow": body,
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"existing-workflow","title":"New Title"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := executeCommand(rootCmd, "workflows", "create", "--file", workflowFile, "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output
	if !strings.Contains(stdout, "Replaced workflow") {
		t.Errorf("expected 'Replaced workflow' message, got: %s", stdout)
	}

	// Verify API calls: GET, DELETE, POST
	if len(apiCalls) != 3 {
		t.Fatalf("expected 3 API calls, got %d: %v", len(apiCalls), apiCalls)
	}
	if apiCalls[0] != "GET /workflows/existing-workflow" {
		t.Errorf("expected GET probe first, got: %s", apiCalls[0])
	}
	if apiCalls[1] != "DELETE /workflows/existing-workflow" {
		t.Errorf("expected DELETE second, got: %s", apiCalls[1])
	}
	if apiCalls[2] != "POST /workflows" {
		t.Errorf("expected POST third, got: %s", apiCalls[2])
	}
}

func TestWorkflowsCreateExistsConfirmYes(t *testing.T) {
	var apiCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/existing-workflow" && r.Method == http.MethodGet {
			apiCalls = append(apiCalls, "GET /workflows/existing-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflow": map[string]interface{}{
					"identifier": "existing-workflow",
					"title":      "Old Title",
				},
			})
		} else if r.URL.Path == "/workflows/existing-workflow" && r.Method == http.MethodDelete {
			apiCalls = append(apiCalls, "DELETE /workflows/existing-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		} else if r.URL.Path == "/workflows" && r.Method == http.MethodPost {
			apiCalls = append(apiCalls, "POST /workflows")
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":       true,
				"workflow": body,
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"existing-workflow","title":"New Title"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inject confirmation: "y\n"
	rootCmd.SetIn(strings.NewReader("y\n"))

	stdout, _, err := executeCommand(rootCmd, "workflows", "create", "--file", workflowFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output
	if !strings.Contains(stdout, "Replaced workflow") {
		t.Errorf("expected 'Replaced workflow' message, got: %s", stdout)
	}

	// Verify API calls: GET, DELETE, POST
	if len(apiCalls) != 3 {
		t.Fatalf("expected 3 API calls, got %d: %v", len(apiCalls), apiCalls)
	}
}

func TestWorkflowsCreateExistsDeclineNo(t *testing.T) {
	var apiCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authHandler(w, r) {
			return
		}
		if r.URL.Path == "/workflows/existing-workflow" && r.Method == http.MethodGet {
			apiCalls = append(apiCalls, "GET /workflows/existing-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflow": map[string]interface{}{
					"identifier": "existing-workflow",
				},
			})
		} else {
			// Should not reach DELETE or POST
			t.Fatalf("unexpected API call: %s %s", r.Method, r.URL.Path)
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"existing-workflow","title":"New Title"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inject confirmation: "n\n"
	rootCmd.SetIn(strings.NewReader("n\n"))

	stdout, _, err := executeCommand(rootCmd, "workflows", "create", "--file", workflowFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output mentions abort
	if !strings.Contains(stdout, "Aborted") {
		t.Errorf("expected 'Aborted' message, got: %s", stdout)
	}

	// Verify only GET happened, no DELETE or POST
	if len(apiCalls) != 1 {
		t.Fatalf("expected 1 API call (GET probe only), got %d: %v", len(apiCalls), apiCalls)
	}
}

func TestWorkflowsCreateFileIdentifierWithSlash(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		// Should never reach API due to pre-flight validation
		t.Fatal("API should not be called for invalid identifier")
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"invalid/id","title":"Test"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCommand(rootCmd, "workflows", "create", "--file", workflowFile)
	if err == nil {
		t.Fatal("expected error for invalid identifier, got nil")
	}

	// Verify it's a validation error
	if !errors.Is(err, ErrInvalidIdentifier) {
		t.Errorf("expected ErrInvalidIdentifier, got: %v", err)
	}
}

func TestWorkflowsCreateFileTooLarge(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		// Should never reach API due to file size check
		t.Fatal("API should not be called for file > 1MB")
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.json")

	// Create file > 1MB
	overhead := len(`{"identifier":"test","data":""}`)
	paddingSize := 1048577 - overhead // 1MB + 1 byte
	padding := strings.Repeat("x", paddingSize)
	content := `{"identifier":"test","data":"` + padding + `"}`

	if err := os.WriteFile(largeFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCommand(rootCmd, "workflows", "create", "--file", largeFile)
	if err == nil {
		t.Fatal("expected error for file > 1MB, got nil")
	}

	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("expected ErrFileTooLarge, got: %v", err)
	}
}

// ====================================================================
// CRITICAL TEST: recreate rollback on POST failure
// ====================================================================

func TestWorkflowsCreateRecreateRollbackOnPostFailure(t *testing.T) {
	var apiCalls []string
	var postBodies []map[string]interface{}

	oldWorkflow := map[string]interface{}{
		"identifier": "test-workflow",
		"title":      "Old Title",
		"data":       "old-data",
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)

		if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodGet {
			// GET probe → return existing workflow
			apiCalls = append(apiCalls, "GET /workflows/test-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":       true,
				"workflow": oldWorkflow,
			})
		} else if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodDelete {
			// DELETE → success
			apiCalls = append(apiCalls, "DELETE /workflows/test-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		} else if r.URL.Path == "/workflows" && r.Method == http.MethodPost {
			// POST
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			postBodies = append(postBodies, body)

			if len(postBodies) == 1 {
				// First POST (new body) → FAIL with 500
				apiCalls = append(apiCalls, "POST /workflows (new, FAILED)")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"ok":      false,
					"message": "Internal server error",
				})
			} else {
				// Second POST (rollback with old body) → SUCCESS
				apiCalls = append(apiCalls, "POST /workflows (rollback, OK)")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"ok":       true,
					"workflow": body,
				})
			}
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	newContent := `{"identifier":"test-workflow","title":"New Title","data":"new-data"}`
	if err := os.WriteFile(workflowFile, []byte(newContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Execute with --force to skip confirmation
	_, _, err := executeCommand(rootCmd, "workflows", "create", "--file", workflowFile, "--force")

	// MUST return an error (the POST failed)
	if err == nil {
		t.Fatal("expected error after POST failure, got nil")
	}

	// Error message should mention rollback
	if !strings.Contains(err.Error(), "rollback") && !strings.Contains(err.Error(), "restored") {
		t.Errorf("expected error to mention rollback, got: %v", err)
	}

	// Verify API call sequence: GET, DELETE, POST (failed), POST (rollback)
	expectedCalls := []string{
		"GET /workflows/test-workflow",
		"DELETE /workflows/test-workflow",
		"POST /workflows (new, FAILED)",
		"POST /workflows (rollback, OK)",
	}
	if len(apiCalls) != len(expectedCalls) {
		t.Fatalf("expected %d API calls, got %d: %v", len(expectedCalls), len(apiCalls), apiCalls)
	}
	for i, expected := range expectedCalls {
		if apiCalls[i] != expected {
			t.Errorf("call %d: expected %q, got %q", i, expected, apiCalls[i])
		}
	}

	// Verify POST bodies: first = new, second = old (rollback)
	if len(postBodies) != 2 {
		t.Fatalf("expected 2 POST bodies, got %d", len(postBodies))
	}

	// First POST body should be the new content
	if postBodies[0]["title"] != "New Title" {
		t.Errorf("first POST should have new title, got: %v", postBodies[0]["title"])
	}
	if postBodies[0]["data"] != "new-data" {
		t.Errorf("first POST should have new data, got: %v", postBodies[0]["data"])
	}

	// Second POST body should be the old content (rollback)
	if postBodies[1]["title"] != "Old Title" {
		t.Errorf("rollback POST should have old title, got: %v", postBodies[1]["title"])
	}
	if postBodies[1]["data"] != "old-data" {
		t.Errorf("rollback POST should have old data, got: %v", postBodies[1]["data"])
	}
}

// ====================================================================
// update command tests
// ====================================================================

func TestWorkflowsUpdateRecreateConfirm(t *testing.T) {
	var apiCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodDelete {
			apiCalls = append(apiCalls, "DELETE /workflows/test-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		} else if r.URL.Path == "/workflows" && r.Method == http.MethodPost {
			apiCalls = append(apiCalls, "POST /workflows")
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":       true,
				"workflow": body,
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"test-workflow","title":"Updated Title"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inject confirmation: "y\n"
	rootCmd.SetIn(strings.NewReader("y\n"))

	stdout, _, err := executeCommand(rootCmd, "workflows", "update", "test-workflow", "--file", workflowFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output
	if !strings.Contains(stdout, "Updated workflow") {
		t.Errorf("expected 'Updated workflow' message, got: %s", stdout)
	}

	// Verify API calls: DELETE, POST
	if len(apiCalls) != 2 {
		t.Fatalf("expected 2 API calls, got %d: %v", len(apiCalls), apiCalls)
	}
	if apiCalls[0] != "DELETE /workflows/test-workflow" {
		t.Errorf("expected DELETE first, got: %s", apiCalls[0])
	}
	if apiCalls[1] != "POST /workflows" {
		t.Errorf("expected POST second, got: %s", apiCalls[1])
	}
}

func TestWorkflowsUpdateIdentifierMismatch(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		// Should never reach API due to mismatch check
		t.Fatal("API should not be called when identifiers mismatch")
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	// File identifier != arg identifier
	content := `{"identifier":"different-workflow","title":"Test"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCommand(rootCmd, "workflows", "update", "test-workflow", "--file", workflowFile)
	if err == nil {
		t.Fatal("expected error for identifier mismatch, got nil")
	}

	// Error message should mention mismatch
	if !strings.Contains(err.Error(), "mismatch") && !strings.Contains(err.Error(), "different") {
		t.Errorf("expected error to mention identifier mismatch, got: %v", err)
	}
}

// ====================================================================
// delete command tests
// ====================================================================

func TestWorkflowsDeleteForce(t *testing.T) {
	var apiCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodDelete {
			apiCalls = append(apiCalls, "DELETE /workflows/test-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	stdout, _, err := executeCommand(rootCmd, "workflows", "delete", "test-workflow", "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output
	if !strings.Contains(stdout, "Deleted workflow") {
		t.Errorf("expected 'Deleted workflow' message, got: %s", stdout)
	}
	if !strings.Contains(stdout, "test-workflow") {
		t.Errorf("expected identifier in output, got: %s", stdout)
	}

	// Verify DELETE happened
	if len(apiCalls) != 1 {
		t.Fatalf("expected 1 API call, got %d: %v", len(apiCalls), apiCalls)
	}
	if apiCalls[0] != "DELETE /workflows/test-workflow" {
		t.Errorf("expected DELETE call, got: %s", apiCalls[0])
	}
}

func TestWorkflowsDeleteDecline(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		// Should never reach DELETE
		t.Fatal("DELETE should not be called when user declines")
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	// Inject confirmation: "n\n"
	rootCmd.SetIn(strings.NewReader("n\n"))

	stdout, _, err := executeCommand(rootCmd, "workflows", "delete", "test-workflow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output mentions abort
	if !strings.Contains(stdout, "Aborted") {
		t.Errorf("expected 'Aborted' message, got: %s", stdout)
	}
}

// ====================================================================
// Command registration tests
// ====================================================================

func TestWorkflowsCommandRegistered(t *testing.T) {
	rootCmd := &cobra.Command{Use: "port"}
	RegisterWorkflows(rootCmd)

	workflowsCmd, _, err := rootCmd.Find([]string{"workflows"})
	if err != nil || workflowsCmd == nil {
		t.Fatal("workflows command not found")
	}

	// Verify subcommands exist
	subcommands := []string{"list", "get", "create", "update", "delete"}
	for _, subcmd := range subcommands {
		cmd, _, err := workflowsCmd.Find([]string{subcmd})
		if err != nil || cmd == nil {
			t.Errorf("subcommand %q not found under workflows", subcmd)
		}
	}
}

func TestWorkflowsCommandAliases(t *testing.T) {
	rootCmd := &cobra.Command{Use: "port"}
	RegisterWorkflows(rootCmd)

	// Verify "workflow" alias works
	workflowCmd, _, err := rootCmd.Find([]string{"workflow"})
	if err != nil || workflowCmd == nil {
		t.Error("'workflow' alias not found")
	}

	// Verify "wf" alias works
	wfCmd, _, err := rootCmd.Find([]string{"wf"})
	if err != nil || wfCmd == nil {
		t.Error("'wf' alias not found")
	}
}

// ====================================================================
// Edge case: stdin exhaustion and error propagation
// ====================================================================

func TestWorkflowsCreateConfirmStdinExhausted(t *testing.T) {
	var apiCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodGet {
			apiCalls = append(apiCalls, "GET /workflows/test-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"workflow": map[string]interface{}{
					"identifier": "test-workflow",
				},
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"test-workflow","title":"Test"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inject empty stdin (EOF immediately)
	rootCmd.SetIn(strings.NewReader(""))

	stdout, _, err := executeCommand(rootCmd, "workflows", "create", "--file", workflowFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// EOF on confirmation should be treated as decline
	if !strings.Contains(stdout, "Aborted") {
		t.Errorf("expected 'Aborted' on EOF, got: %s", stdout)
	}

	// Only GET probe should have happened
	if len(apiCalls) != 1 {
		t.Fatalf("expected 1 API call (GET probe only), got %d: %v", len(apiCalls), apiCalls)
	}
}

// ====================================================================
// Additional edge case: update rollback on POST failure
// ====================================================================

func TestWorkflowsUpdateRecreateRollbackOnPostFailure(t *testing.T) {
	var apiCalls []string
	var postBodies []map[string]interface{}

	// Simulate: update has no GET probe, but should handle POST failure with rollback
	// However, update does NOT have GET probe — it directly does DELETE + POST.
	// If POST fails, there's no oldWorkflow to rollback to UNLESS we fetch it first.
	// Based on the spec, update does NOT do a GET probe — it trusts the identifier.
	// So rollback is only relevant for create (which does GET probe).
	// This test verifies that update WITHOUT GET probe gracefully handles POST failure
	// but CANNOT rollback (no old state fetched).

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandler(w, r)
		if r.URL.Path == "/workflows/test-workflow" && r.Method == http.MethodDelete {
			apiCalls = append(apiCalls, "DELETE /workflows/test-workflow")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		} else if r.URL.Path == "/workflows" && r.Method == http.MethodPost {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			postBodies = append(postBodies, body)

			apiCalls = append(apiCalls, "POST /workflows")
			// POST fails
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":      false,
				"message": "Internal server error",
			})
		}
	})
	rootCmd, _, cleanup := setupTestCommand(handler)
	defer cleanup()

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"test-workflow","title":"Updated"}`
	if err := os.WriteFile(workflowFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Execute update with --force
	_, _, err := executeCommand(rootCmd, "workflows", "update", "test-workflow", "--file", workflowFile, "--force")

	// MUST return an error (POST failed)
	if err == nil {
		t.Fatal("expected error after POST failure, got nil")
	}

	// Verify DELETE happened, POST happened (failed), NO rollback (no GET probe)
	expectedCalls := []string{
		"DELETE /workflows/test-workflow",
		"POST /workflows",
	}
	if len(apiCalls) != len(expectedCalls) {
		t.Fatalf("expected %d API calls, got %d: %v", len(expectedCalls), len(apiCalls), apiCalls)
	}

	// NOTE: For update, rollback is NOT possible without GET probe.
	// If the spec requires rollback for update too, we'd need to add a GET probe before DELETE.
	// Current spec only mentions rollback for create (which has GET probe).
}
