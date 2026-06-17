package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/port-experimental/port-cli/internal/auth"
)

func TestGetSkillVersionsForSkills_PaginatesSearchResults(t *testing.T) {
	var requestPaths []string
	var requestBodies []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestPaths = append(requestPaths, r.URL.Path)
		var requestBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodies = append(requestBodies, requestBody)
		if requestBody["from"] == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":   true,
				"next": "cursor-2",
				"entities": []map[string]interface{}{
					{"identifier": "version-1"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entities": []map[string]interface{}{
				{"identifier": "version-2"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entities, err := client.GetSkillVersionsForSkills(context.Background(), []string{"skill-a", "skill-b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 2 || entities[0]["identifier"] != "version-1" || entities[1]["identifier"] != "version-2" {
		t.Fatalf("unexpected entities: %+v", entities)
	}
	if len(requestPaths) != 2 || requestPaths[0] != "/blueprints/skill_version/entities/search" || requestPaths[1] != "/blueprints/skill_version/entities/search" {
		t.Fatalf("expected two search requests, got %v", requestPaths)
	}
	if requestBodies[0]["limit"] != float64(1000) {
		t.Errorf("expected limit 1000, got %#v", requestBodies[0]["limit"])
	}
	if requestBodies[1]["from"] != "cursor-2" {
		t.Fatalf("expected second request to use cursor, got %#v", requestBodies[1])
	}
	query := requestBodies[0]["query"].(map[string]interface{})
	rules := query["rules"].([]interface{})
	rule := rules[0].(map[string]interface{})
	if rule["operator"] != "matchAny" {
		t.Errorf("expected matchAny rule, got %#v", rule)
	}
	value := rule["value"].([]interface{})
	if len(value) != 2 || value[0] != "skill-a" || value[1] != "skill-b" {
		t.Errorf("unexpected skill filter value: %#v", value)
	}
}

func TestGetSkillFilesForVersions_UsesRelationPathSearch(t *testing.T) {
	var requestPaths []string
	var requestBodies []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestPaths = append(requestPaths, r.URL.Path)
		var requestBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestBodies = append(requestBodies, requestBody)
		if requestBody["from"] == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":   true,
				"next": "cursor-2",
				"entities": []map[string]interface{}{
					{"identifier": "file-1"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entities": []map[string]interface{}{
				{"identifier": "file-2"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entities, err := client.GetSkillFilesForVersions(context.Background(), []string{"version-a", "version-b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 2 || entities[0]["identifier"] != "file-1" || entities[1]["identifier"] != "file-2" {
		t.Fatalf("unexpected entities: %+v", entities)
	}
	if len(requestPaths) != 2 || requestPaths[0] != "/blueprints/skill_file/entities/search" || requestPaths[1] != "/blueprints/skill_file/entities/search" {
		t.Fatalf("expected search endpoint, got %v", requestPaths)
	}
	if requestBodies[1]["from"] != "cursor-2" {
		t.Fatalf("expected second request to use cursor, got %#v", requestBodies[1])
	}
	query := requestBodies[0]["query"].(map[string]interface{})
	rules := query["rules"].([]interface{})
	rule := rules[0].(map[string]interface{})
	if rule["operator"] != "matchAny" {
		t.Errorf("expected matchAny rule, got %#v", rule)
	}
	value := rule["value"].([]interface{})
	if len(value) != 2 || value[0] != "version-a" || value[1] != "version-b" {
		t.Errorf("unexpected version filter value: %#v", value)
	}
}

func TestGetSkills_IncludesSkillGroupRelation(t *testing.T) {
	var requestPath string
	var requestBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entities": []map[string]interface{}{
				{"identifier": "skill-a"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entities, err := client.GetSkills(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 1 || entities[0]["identifier"] != "skill-a" {
		t.Fatalf("unexpected entities: %+v", entities)
	}
	if requestPath != "/blueprints/skill/entities/search" {
		t.Fatalf("expected skill search endpoint, got %s", requestPath)
	}
	include := requestBody["include"].([]interface{})
	found := false
	for _, item := range include {
		if item == "skill_to_skill_group" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected include to contain skill_to_skill_group, got %#v", include)
	}
	for _, expected := range []string{"description", "instructions", "references", "assets", "scripts", "additional_files"} {
		found := false
		for _, item := range include {
			if item == expected {
				found = true
				break
			}
		}
		if found {
			t.Fatalf("expected include to avoid optional legacy field %s, got %#v", expected, include)
		}
	}
}

func TestGetSkills_FallsBackToLegacyEntitiesWhenRelationMissing(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/blueprints/skill/entities/search" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":      false,
				"error":   "invalid_request",
				"message": "Some of the properties you are trying to include are not valid: skill_to_skill_group",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entities": []map[string]interface{}{
				{"identifier": "legacy-skill"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entities, err := client.GetSkills(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 1 || entities[0]["identifier"] != "legacy-skill" {
		t.Fatalf("unexpected entities: %+v", entities)
	}
	expected := []string{"/blueprints/skill/entities/search", "/blueprints/skill/entities"}
	if len(paths) != len(expected) {
		t.Fatalf("expected paths %v, got %v", expected, paths)
	}
	for i := range expected {
		if paths[i] != expected[i] {
			t.Fatalf("expected paths %v, got %v", expected, paths)
		}
	}
}

func TestGetBlueprintPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		if r.URL.Path == "/blueprints/service/permissions" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"permissions": map[string]interface{}{
					"entities": map[string]interface{}{"view": []string{"$team"}, "create": []string{"$admin"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	perms, err := client.GetBlueprintPermissions(context.Background(), "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perms["entities"] == nil {
		t.Error("expected entities permissions")
	}
}

func TestGetActionPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		if r.URL.Path == "/actions/deploy/permissions" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"permissions": map[string]interface{}{
					"execute": map[string]interface{}{"users": []string{"alice@example.com"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	perms, err := client.GetActionPermissions(context.Background(), "deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perms["execute"] == nil {
		t.Error("expected execute permissions")
	}
}

func TestUpdateBlueprintPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		if r.URL.Path == "/blueprints/service/permissions" {
			if r.Method != http.MethodPatch {
				http.Error(w, "expected PATCH", http.StatusMethodNotAllowed)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"permissions": map[string]interface{}{
					"entities": map[string]interface{}{"view": []string{"$admin"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	perms, err := client.UpdateBlueprintPermissions(context.Background(), "service", Permissions{
		"entities": map[string]interface{}{"view": []string{"$admin"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perms["entities"] == nil {
		t.Error("expected entities in updated permissions")
	}
}

func TestUpdateActionPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		if r.URL.Path == "/actions/deploy/permissions" {
			if r.Method != http.MethodPatch {
				http.Error(w, "expected PATCH", http.StatusMethodNotAllowed)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"permissions": map[string]interface{}{
					"execute": map[string]interface{}{"users": []string{"alice@example.com"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	perms, err := client.UpdateActionPermissions(context.Background(), "deploy", Permissions{
		"execute": map[string]interface{}{"users": []string{"alice@example.com"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perms["execute"] == nil {
		t.Error("expected execute in updated permissions")
	}
}

func TestGetBlueprintPermissionsWithToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false})
			t.Fatal("unexpected call to /auth/access_token")
			return
		}
		if r.URL.Path == "/blueprints/service/permissions" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"permissions": map[string]interface{}{
					"entities": map[string]interface{}{"view": []string{"$team"}, "create": []string{"$admin"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	exp := time.Now().Add(time.Hour * 24).Unix()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"aud":                             "https://api.example.com",
		"exp":                             float64(exp),
		"https://api.example.com/email":   "user@test.com",
		"https://api.example.com/orgId":   "someOrgId",
		"https://api.example.com/orgName": "Org Name",
	})
	signed, err := token.SignedString([]byte("signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := auth.ParseToken(signed)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(ClientOpts{Token: parsed, APIURL: server.URL, Timeout: 0})
	perms, err := client.GetBlueprintPermissions(context.Background(), "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perms["entities"] == nil {
		t.Error("expected entities permissions")
	}
}

func TestCallGenericGETAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}

		if r.Method != "GET" {
			t.Fatalf("unexpected %s call", r.Method)
			return
		}
		if r.URL.Path == "/actions/runs" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	res, err := client.Request(context.Background(), RequestParams{Method: "GET", Endpoint: "/actions/runs"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res, ok := res.(map[string]any); ok && res["ok"] != true {
		t.Error("expected entities permissions")
	}
}

func TestCallGenericPOSTAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}

		if r.Method != "POST" {
			t.Fatalf("unexpected %s call", r.Method)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("error reading body %v", err)
			return
		}
		if string(body) != `{"properties":{}}` {
			t.Fatalf("unexpected body '%s'", string(body))
			return
		}
		if r.URL.Path == "/actions/my-action/runs" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	res, err := client.Request(
		context.Background(), RequestParams{
			Method:   "POST",
			Data:     map[string]any{"properties": map[string]any{}},
			Endpoint: "/actions/my-action/runs",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res, ok := res.(map[string]any); ok && res["ok"] != true {
		t.Error("expected entities permissions")
	}
}

/**
 * @spec-handoff
 * @interface GetWorkflows(ctx context.Context) ([]map[string]interface{}, error)
 * @interface GetWorkflow(ctx context.Context, identifier string) (map[string]interface{}, error)
 * @interface CreateWorkflow(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error)
 * @interface DeleteWorkflow(ctx context.Context, identifier string) error
 * @behavior
 *   - GetWorkflows: GET /workflows → unwrap {"ok":true,"workflows":[...]} to slice
 *   - GetWorkflow: GET /workflows/{identifier} → unwrap {"ok":true,"workflow":{...}} to map
 *   - GetWorkflow on 404: return sentinel error ErrWorkflowNotFound (detectable via errors.Is)
 *   - CreateWorkflow: POST /workflows with body JSON → unwrap {"ok":true,"workflow":{...}} to map
 *   - DeleteWorkflow: DELETE /workflows/{identifier} → nil on success, error on failure
 * @edge-cases
 *   - GetWorkflow 404 → errors.Is(err, ErrWorkflowNotFound) must be true
 *   - All paths must start with /workflows and MUST NOT start with /v1/
 * @sentinel-error
 *   - ErrWorkflowNotFound = errors.New("workflow not found") for 404 detection
 * @see internal/api/requests.go
 */

func TestGetWorkflows(t *testing.T) {
	var requestMethod, requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestMethod = r.Method
		requestPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"workflows": []map[string]interface{}{
				{"identifier": "workflow-1"},
				{"identifier": "workflow-2"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	workflows, err := client.GetWorkflows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestMethod != http.MethodGet {
		t.Errorf("expected GET, got %s", requestMethod)
	}
	if requestPath != "/workflows" {
		t.Errorf("expected /workflows, got %s", requestPath)
	}
	if len(workflows) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(workflows))
	}
	if workflows[0]["identifier"] != "workflow-1" || workflows[1]["identifier"] != "workflow-2" {
		t.Fatalf("unexpected workflow identifiers: %+v", workflows)
	}
}

func TestGetWorkflow(t *testing.T) {
	var requestMethod, requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestMethod = r.Method
		requestPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"workflow": map[string]interface{}{
				"identifier": "my-workflow",
				"name":       "My Workflow",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	workflow, err := client.GetWorkflow(context.Background(), "my-workflow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestMethod != http.MethodGet {
		t.Errorf("expected GET, got %s", requestMethod)
	}
	if requestPath != "/workflows/my-workflow" {
		t.Errorf("expected /workflows/my-workflow, got %s", requestPath)
	}
	if workflow["identifier"] != "my-workflow" {
		t.Errorf("expected identifier my-workflow, got %v", workflow["identifier"])
	}
}

func TestGetWorkflow_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"message": "Workflow not found",
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.GetWorkflow(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrWorkflowNotFound) {
		t.Errorf("expected ErrWorkflowNotFound via errors.Is, got: %v", err)
	}
}

func TestCreateWorkflow(t *testing.T) {
	var requestMethod, requestPath string
	var requestBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestMethod = r.Method
		requestPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"workflow": map[string]interface{}{
				"identifier": requestBody["identifier"],
				"name":       requestBody["name"],
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	inputBody := map[string]interface{}{
		"identifier": "new-workflow",
		"name":       "New Workflow",
	}
	workflow, err := client.CreateWorkflow(context.Background(), inputBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", requestMethod)
	}
	if requestPath != "/workflows" {
		t.Errorf("expected /workflows, got %s", requestPath)
	}
	if requestBody["identifier"] != "new-workflow" {
		t.Errorf("expected server to receive identifier new-workflow, got %v", requestBody["identifier"])
	}
	if workflow["identifier"] != "new-workflow" {
		t.Errorf("expected returned identifier new-workflow, got %v", workflow["identifier"])
	}
}

func TestDeleteWorkflow(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectError    bool
		workflowExists bool
	}{
		{
			name:           "success",
			statusCode:     http.StatusOK,
			expectError:    false,
			workflowExists: true,
		},
		{
			name:           "not found",
			statusCode:     http.StatusNotFound,
			expectError:    true,
			workflowExists: false,
		},
		{
			name:           "server error",
			statusCode:     http.StatusInternalServerError,
			expectError:    true,
			workflowExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestMethod, requestPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/access_token" {
					json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
					return
				}
				requestMethod = r.Method
				requestPath = r.URL.Path
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
				} else {
					json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "message": "error"})
				}
			}))
			defer server.Close()

			client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
			err := client.DeleteWorkflow(context.Background(), "workflow-id")
			if tt.expectError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if requestMethod != http.MethodDelete {
				t.Errorf("expected DELETE, got %s", requestMethod)
			}
			if requestPath != "/workflows/workflow-id" {
				t.Errorf("expected /workflows/workflow-id, got %s", requestPath)
			}
		})
	}
}

func TestWorkflowPathsHaveNoV1Prefix(t *testing.T) {
	tests := []struct {
		name         string
		invokeMethod func(client *Client) error
		expectedPath string
	}{
		{
			name: "GetWorkflows",
			invokeMethod: func(client *Client) error {
				_, err := client.GetWorkflows(context.Background())
				return err
			},
			expectedPath: "/workflows",
		},
		{
			name: "GetWorkflow",
			invokeMethod: func(client *Client) error {
				_, err := client.GetWorkflow(context.Background(), "test-id")
				return err
			},
			expectedPath: "/workflows/test-id",
		},
		{
			name: "CreateWorkflow",
			invokeMethod: func(client *Client) error {
				_, err := client.CreateWorkflow(context.Background(), map[string]interface{}{"identifier": "test"})
				return err
			},
			expectedPath: "/workflows",
		},
		{
			name: "DeleteWorkflow",
			invokeMethod: func(client *Client) error {
				return client.DeleteWorkflow(context.Background(), "test-id")
			},
			expectedPath: "/workflows/test-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/access_token" {
					json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
					return
				}
				capturedPath = r.URL.Path
				json.NewEncoder(w).Encode(map[string]interface{}{
					"ok":       true,
					"workflow": map[string]interface{}{"identifier": "test"},
					"workflows": []map[string]interface{}{
						{"identifier": "test"},
					},
				})
			}))
			defer server.Close()

			client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
			_ = tt.invokeMethod(client)

			if capturedPath != tt.expectedPath {
				t.Errorf("expected path %s, got %s", tt.expectedPath, capturedPath)
			}
			if len(capturedPath) >= 4 && capturedPath[:4] == "/v1/" {
				t.Errorf("path must NOT start with /v1/, got %s", capturedPath)
			}
			if len(tt.expectedPath) >= 10 && tt.expectedPath[:10] != "/workflows" {
				t.Errorf("expected path to start with /workflows, got %s", tt.expectedPath)
			}
		})
	}
}

/**
 * @spec-handoff
 * @interface CreateEntityWithParams(ctx context.Context, blueprintIdentifier string, entity Entity, upsert, merge bool) (Entity, error)
 * @interface PatchEntity(ctx context.Context, blueprintIdentifier, entityIdentifier string, patch Entity) (Entity, error)
 * @behavior CreateEntityWithParams
 *   - POSTs to /blueprints/{blueprintIdentifier}/entities (NO /v1 prefix — base URL includes it)
 *   - When upsert=false: adds query param ?upsert=false, NO merge param
 *   - When upsert=true, merge=false: adds query params ?upsert=true&merge=false
 *   - When upsert=true, merge=true: adds query params ?upsert=true&merge=true
 *   - Sends entity as JSON request body
 *   - Unwraps response {"entity": {...}} and returns the entity
 *   - Returns error on HTTP 409 (conflict), 401, or other non-2xx
 * @behavior PatchEntity
 *   - PATCHes to /blueprints/{blueprintIdentifier}/entities/{entityIdentifier} (NO /v1 prefix)
 *   - Sends patch as JSON request body
 *   - Unwraps response {"entity": {...}} and returns the entity
 *   - Returns error on HTTP 404 or other non-2xx
 * @edge-cases
 *   - 401 response: error must NOT embed response body (security)
 *   - 409 conflict: returns non-nil error
 *   - 404 not found: returns non-nil error
 * @see CreateEntity (lines 264-279) for existing pattern
 * @see PatchBlueprint (lines 136-152) for PATCH pattern
 */

func TestCreateEntityWithParams_UpsertFalse(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedQuery string
	var receivedBody Entity
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entity": map[string]interface{}{
				"identifier": "test-entity-1",
				"title":      "Test Entity",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entity, err := client.CreateEntityWithParams(context.Background(), "my-blueprint", Entity{
		"identifier": "test-entity-1",
		"title":      "Test Entity",
	}, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedMethod != "POST" {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if receivedPath != "/blueprints/my-blueprint/entities" {
		t.Errorf("expected /blueprints/my-blueprint/entities, got %s", receivedPath)
	}
	if receivedQuery != "upsert=false" {
		t.Errorf("expected query 'upsert=false' (no merge), got '%s'", receivedQuery)
	}
	if receivedBody["identifier"] != "test-entity-1" {
		t.Errorf("unexpected request body: %+v", receivedBody)
	}
	if entity["identifier"] != "test-entity-1" {
		t.Errorf("expected entity identifier test-entity-1, got %+v", entity)
	}
}

func TestCreateEntityWithParams_UpsertTrueMergeFalse(t *testing.T) {
	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		receivedQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entity": map[string]interface{}{
				"identifier": "upserted-entity",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entity, err := client.CreateEntityWithParams(context.Background(), "my-blueprint", Entity{
		"identifier": "upserted-entity",
	}, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Query must include both upsert=true and merge=false
	if receivedQuery != "upsert=true&merge=false" && receivedQuery != "merge=false&upsert=true" {
		t.Errorf("expected query 'upsert=true&merge=false', got '%s'", receivedQuery)
	}
	if entity["identifier"] != "upserted-entity" {
		t.Errorf("unexpected entity: %+v", entity)
	}
}

func TestCreateEntityWithParams_UpsertTrueMergeTrue(t *testing.T) {
	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		receivedQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entity": map[string]interface{}{
				"identifier": "merged-entity",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entity, err := client.CreateEntityWithParams(context.Background(), "my-blueprint", Entity{
		"identifier": "merged-entity",
	}, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Query must include both upsert=true and merge=true
	if receivedQuery != "upsert=true&merge=true" && receivedQuery != "merge=true&upsert=true" {
		t.Errorf("expected query 'upsert=true&merge=true', got '%s'", receivedQuery)
	}
	if entity["identifier"] != "merged-entity" {
		t.Errorf("unexpected entity: %+v", entity)
	}
}

func TestCreateEntityWithParams_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"message": "entity already exists",
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.CreateEntityWithParams(context.Background(), "my-blueprint", Entity{
		"identifier": "conflict-entity",
	}, false, false)
	if err == nil {
		t.Error("expected error on 409 conflict, got nil")
	}
}

func TestCreateEntityWithParams_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"message": "unauthorized secret body content",
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.CreateEntityWithParams(context.Background(), "my-blueprint", Entity{
		"identifier": "unauth-entity",
	}, false, false)
	if err == nil {
		t.Error("expected error on 401 unauthorized, got nil")
	}
	// Per convention, 401 errors must NOT embed response body (security)
	// If the error message contains "secret body content", that's a leak
	if err != nil && strings.Contains(err.Error(), "secret body content") {
		t.Errorf("401 error must not leak response body, got: %v", err)
	}
}

func TestPatchEntity_HappyPath(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedBody Entity
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entity": map[string]interface{}{
				"identifier": "patched-entity",
				"title":      "Updated Title",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entity, err := client.PatchEntity(context.Background(), "my-blueprint", "patched-entity", Entity{
		"title": "Updated Title",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedMethod != "PATCH" {
		t.Errorf("expected PATCH, got %s", receivedMethod)
	}
	if receivedPath != "/blueprints/my-blueprint/entities/patched-entity" {
		t.Errorf("expected /blueprints/my-blueprint/entities/patched-entity, got %s", receivedPath)
	}
	if receivedBody["title"] != "Updated Title" {
		t.Errorf("unexpected patch body: %+v", receivedBody)
	}
	if entity["identifier"] != "patched-entity" {
		t.Errorf("expected entity identifier patched-entity, got %+v", entity)
	}
	if entity["title"] != "Updated Title" {
		t.Errorf("expected entity title 'Updated Title', got %+v", entity)
	}
}

func TestPatchEntity_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"message": "entity not found",
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.PatchEntity(context.Background(), "my-blueprint", "missing-entity", Entity{
		"title": "Will Fail",
	})
	if err == nil {
		t.Error("expected error on 404 not found, got nil")
	}
}

// ====================================================================
// /v1 double-prefix regression tests (should PASS now)
// ====================================================================

func TestCreateEntityWithParams_PathHasNoV1Prefix(t *testing.T) {
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		receivedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entity": map[string]interface{}{
				"identifier": "test-entity",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.CreateEntityWithParams(context.Background(), "my-blueprint", Entity{
		"identifier": "test-entity",
	}, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Assert path starts with /blueprints/ and does NOT start with /v1/
	if !strings.HasPrefix(receivedPath, "/blueprints/") {
		t.Errorf("expected path to start with /blueprints/, got %s", receivedPath)
	}
	if strings.HasPrefix(receivedPath, "/v1/") {
		t.Errorf("path must NOT start with /v1/ (base URL already includes it), got %s", receivedPath)
	}
}

func TestPatchEntity_PathHasNoV1Prefix(t *testing.T) {
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		receivedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"entity": map[string]interface{}{
				"identifier": "test-entity",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.PatchEntity(context.Background(), "my-blueprint", "test-entity", Entity{
		"title": "Updated",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Assert path starts with /blueprints/ and does NOT start with /v1/
	if !strings.HasPrefix(receivedPath, "/blueprints/") {
		t.Errorf("expected path to start with /blueprints/, got %s", receivedPath)
	}
	if strings.HasPrefix(receivedPath, "/v1/") {
		t.Errorf("path must NOT start with /v1/ (base URL already includes it), got %s", receivedPath)
	}
}

// ====================================================================
// 401 positive assertion improvement
// ====================================================================

func TestCreateEntityWithParams_UnauthorizedPositiveAssertion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"message": "unauthorized secret body content",
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.CreateEntityWithParams(context.Background(), "my-blueprint", Entity{
		"identifier": "unauth-entity",
	}, false, false)
	// Positive assertions: error is non-nil and mentions 401/unauthorized/failed
	if err == nil {
		t.Fatal("expected error on 401 unauthorized, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "401") && !strings.Contains(errMsg, "unauthorized") && !strings.Contains(errMsg, "Unauthorized") && !strings.Contains(errMsg, "failed") {
		t.Errorf("expected error message to mention 401/unauthorized/failed, got: %v", errMsg)
	}
	// Negative assertion: must NOT leak response body
	if strings.Contains(errMsg, "secret body content") {
		t.Errorf("401 error must not leak response body, got: %v", err)
	}
}
