package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// @spec-handoff
// @interface UpsertSkillEntity(ctx context.Context, entity Entity, upsert bool, merge bool) (Entity, error)
// @interface PatchSkillEntity(ctx context.Context, identifier string, entity Entity) (Entity, error)
//
// @behavior
//   - UpsertSkillEntity issues POST /blueprints/skill/entities with query params
//     upsert=<upsert>&merge=<merge>; body is the Entity; response is {"entity":{...}}.
//   - PatchSkillEntity issues PATCH /blueprints/skill/entities/<identifier>;
//     body is the Entity; response is {"entity":{...}}.
//   - Both methods decode the "entity" wrapper and return the inner Entity.
//   - Both methods return a non-nil error when the server responds with 4xx/5xx.
//
// @edge-cases
//   - Path MUST NOT include /v1 — the base URL already contains it.
//     Assert r.URL.Path == "/blueprints/skill/entities" (no /v1 prefix).
//   - UpsertSkillEntity, merge=true (default): query param merge=true.
//   - UpsertSkillEntity, merge=false (force replace): query param merge=false.
//   - Server 500 → err != nil, error string carries the HTTP status.
//   - Server 404 → err != nil.
//
// @see internal/api/requests.go — CreateEntity for analogous POST+entity-wrapper pattern.
// @see internal/api/client.go  — request() for query-param wiring via params map[string]string.

func TestUpsertSkillEntity_DefaultMerge(t *testing.T) {
	var requestCount int
	var capturedMethod string
	var capturedPath string
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestCount++
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{
				"identifier": "skill-upsert-1",
				"title":      "Upserted Skill",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entity, err := client.UpsertSkillEntity(context.Background(), Entity{"identifier": "skill-upsert-1", "title": "Upserted Skill"}, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 request, got %d", requestCount)
	}
	if capturedMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/blueprints/skill/entities" {
		t.Fatalf("expected path /blueprints/skill/entities (no /v1), got %s", capturedPath)
	}
	q, err := url.ParseQuery(capturedQuery)
	if err != nil {
		t.Fatalf("failed to parse query: %v", err)
	}
	if q.Get("upsert") != "true" {
		t.Errorf("expected upsert=true, got %q", q.Get("upsert"))
	}
	if q.Get("merge") != "true" {
		t.Errorf("expected merge=true, got %q", q.Get("merge"))
	}
	if entity["identifier"] != "skill-upsert-1" {
		t.Errorf("expected identifier skill-upsert-1, got %v", entity["identifier"])
	}
}

func TestUpsertSkillEntity_ForceReplace(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		capturedQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{
				"identifier": "skill-upsert-2",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.UpsertSkillEntity(context.Background(), Entity{"identifier": "skill-upsert-2"}, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, err := url.ParseQuery(capturedQuery)
	if err != nil {
		t.Fatalf("failed to parse query: %v", err)
	}
	if q.Get("merge") != "false" {
		t.Errorf("expected merge=false (force replace), got %q", q.Get("merge"))
	}
	if q.Get("upsert") != "true" {
		t.Errorf("expected upsert=true, got %q", q.Get("upsert"))
	}
}

func TestUpsertSkillEntity_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "internal_error"})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.UpsertSkillEntity(context.Background(), Entity{"identifier": "skill-fail"}, true, true)
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") && !strings.Contains(err.Error(), "Internal Server Error") {
		t.Errorf("expected error to contain HTTP status, got: %v", err)
	}
}

func TestPatchSkillEntity_Success(t *testing.T) {
	var requestCount int
	var capturedMethod string
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		requestCount++
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": map[string]interface{}{
				"identifier": "skill-patch-1",
				"title":      "Patched Skill",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	entity, err := client.PatchSkillEntity(context.Background(), "skill-patch-1", Entity{"title": "Patched Skill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 request, got %d", requestCount)
	}
	if capturedMethod != http.MethodPatch {
		t.Fatalf("expected PATCH, got %s", capturedMethod)
	}
	if capturedPath != "/blueprints/skill/entities/skill-patch-1" {
		t.Fatalf("expected path /blueprints/skill/entities/skill-patch-1, got %s", capturedPath)
	}
	if entity["identifier"] != "skill-patch-1" {
		t.Errorf("expected identifier skill-patch-1, got %v", entity["identifier"])
	}
	if entity["title"] != "Patched Skill" {
		t.Errorf("expected title Patched Skill, got %v", entity["title"])
	}
}

func TestPatchSkillEntity_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/access_token" {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accessToken": "tok", "expiresIn": 3600})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "not_found"})
	}))
	defer server.Close()

	client := NewClient(ClientOpts{ClientID: "id", ClientSecret: "secret", APIURL: server.URL, Timeout: 0})
	_, err := client.PatchSkillEntity(context.Background(), "nonexistent-skill", Entity{"title": "Ghost"})
	if err == nil {
		t.Fatal("expected error from 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Errorf("expected error to contain HTTP status, got: %v", err)
	}
}
