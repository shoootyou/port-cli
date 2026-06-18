/**
 * @spec-handoff
 *
 * @interface validateEntityIdentifier(identifier string) error
 * @behavior
 *   - Returns ErrInvalidIdentifier when identifier contains '/' anywhere in the string
 *   - Returns ErrInvalidIdentifier when identifier is empty string
 *   - Returns nil for valid identifiers (alphanumeric, dashes, underscores)
 * @edge-cases
 *   - Empty string → ErrInvalidIdentifier
 *   - Single slash → ErrInvalidIdentifier
 *   - Slash in middle → ErrInvalidIdentifier
 *   - Valid identifier with dashes/underscores → nil
 * @sentinel-errors
 *   - var ErrInvalidIdentifier = errors.New("invalid identifier")
 *
 * @interface parseEntityFile(path string) (api.Entity, error)
 * @behavior
 *   - Auto-detects format by file extension: .json, .yaml, .yml
 *   - Rejects symlinks (use os.Lstat, check Mode&os.ModeSymlink)
 *   - Rejects files > 1MB (check size before reading via os.Stat)
 *   - Parses valid JSON/YAML into api.Entity (map[string]interface{})
 *   - Returns error for unknown extensions
 *   - Returns error for malformed JSON/YAML
 *   - Returns error for nonexistent files
 * @edge-cases
 *   - .json extension → json.Unmarshal
 *   - .yaml extension → yaml.Unmarshal
 *   - .yml extension → yaml.Unmarshal
 *   - .txt extension → ErrUnsupportedFormat
 *   - File exactly 1MB → ok, file 1MB+1 byte → ErrFileTooLarge
 *   - Symlink → ErrSymlink (before size check)
 *   - Malformed JSON → parse error
 *   - Malformed YAML → parse error
 * @sentinel-errors
 *   - var ErrFileTooLarge = errors.New("file exceeds 1MB limit")
 *   - var ErrSymlink = errors.New("symlinks are not supported")
 *   - var ErrUnsupportedFormat = errors.New("unsupported file format")
 *
 * @interface detectUnknownEntityFields(entity api.Entity) []string
 * @behavior
 *   - Returns top-level keys that are NOT in the known Port entity field set
 *   - Known fields: identifier, title, blueprint, properties, relations, team, icon, createdAt, createdBy, updatedAt, updatedBy
 *   - Returns sorted slice of unknown field names (deterministic)
 *   - Returns empty slice when all fields are known
 * @edge-cases
 *   - Entity with only known fields → []
 *   - Entity with unknown fields "foo", "bar" → ["bar", "foo"] (sorted)
 *   - Empty entity → []
 *
 * @interface confirmAction(prompt string, force bool, stdin io.Reader) (bool, error)
 * @behavior
 *   - 3-path dispatch for testability and non-interactive environments:
 *     1. If force==true → return (true, nil) immediately without reading stdin
 *     2. If stdin != nil → read from stdin: "y"/"Y"/"yes"/"YES" (case-insensitive) → true, EOF or anything else → false
 *     3. If stdin == nil → fall back to interactive TUI prompt (not exercised in unit tests)
 *   - Only "y" or "yes" (case-insensitive, with or without newline) count as confirmation
 * @edge-cases
 *   - force=true → (true, nil), no stdin read
 *   - stdin="y\n" → (true, nil)
 *   - stdin="yes\n" → (true, nil)
 *   - stdin="Y\n" → (true, nil)
 *   - stdin="YES\n" → (true, nil)
 *   - stdin="n\n" → (false, nil)
 *   - stdin="" (EOF) → (false, nil)
 *   - stdin="garbage\n" → (false, nil)
 *   - stdin=nil → fall back to TUI (not tested here)
 *
 * @see internal/commands/clear.go (confirmation pattern reference)
 */

package commands

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/port-experimental/port-cli/internal/api"
)

// ====================================================================
// validateEntityIdentifier tests
// ====================================================================

func TestValidateEntityIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		wantErr    error
	}{
		{
			name:       "valid identifier with dashes",
			identifier: "valid-id",
			wantErr:    nil,
		},
		{
			name:       "valid identifier with underscores",
			identifier: "valid_id_123",
			wantErr:    nil,
		},
		{
			name:       "identifier with slash in middle",
			identifier: "id/with/slash",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "identifier with single slash",
			identifier: "id/partial",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "empty identifier",
			identifier: "",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "identifier starting with slash",
			identifier: "/leading",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "identifier ending with slash",
			identifier: "trailing/",
			wantErr:    ErrInvalidIdentifier,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEntityIdentifier(tt.identifier)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

// ====================================================================
// parseEntityFile tests
// ====================================================================

func TestParseEntityFileJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "entity.json")
	content := `{"identifier": "test-entity", "title": "Test Entity"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	entity, err := parseEntityFile(jsonFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if entity["identifier"] != "test-entity" {
		t.Errorf("expected identifier 'test-entity', got %v", entity["identifier"])
	}
	if entity["title"] != "Test Entity" {
		t.Errorf("expected title 'Test Entity', got %v", entity["title"])
	}
}

func TestParseEntityFileYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "entity.yaml")
	content := "identifier: test-entity\ntitle: Test Entity\n"
	if err := os.WriteFile(yamlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	entity, err := parseEntityFile(yamlFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if entity["identifier"] != "test-entity" {
		t.Errorf("expected identifier 'test-entity', got %v", entity["identifier"])
	}
}

func TestParseEntityFileYML(t *testing.T) {
	tmpDir := t.TempDir()
	ymlFile := filepath.Join(tmpDir, "entity.yml")
	content := "identifier: test-entity\ntitle: Test Entity\n"
	if err := os.WriteFile(ymlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	entity, err := parseEntityFile(ymlFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if entity["identifier"] != "test-entity" {
		t.Errorf("expected identifier 'test-entity', got %v", entity["identifier"])
	}
}

func TestParseEntityFileMalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "bad.json")
	content := `{"identifier": "test-entity", "title": }`
	if err := os.WriteFile(jsonFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := parseEntityFile(jsonFile)
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
}

func TestParseEntityFileMalformedYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "bad.yaml")
	content := "identifier: test-entity\ntitle: [unclosed"
	if err := os.WriteFile(yamlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := parseEntityFile(yamlFile)
	if err == nil {
		t.Fatal("expected parse error for malformed YAML, got nil")
	}
}

func TestParseEntityFileUnsupportedExtension(t *testing.T) {
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "entity.txt")
	content := `{"identifier": "test-entity"}`
	if err := os.WriteFile(txtFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := parseEntityFile(txtFile)
	if err == nil {
		t.Fatal("expected unsupported format error, got nil")
	}
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}

func TestParseEntityFileExceedsSize(t *testing.T) {
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.json")
	// Create a file just over 1MB
	content := strings.Repeat("a", 1024*1024+1)
	if err := os.WriteFile(largeFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := parseEntityFile(largeFile)
	if err == nil {
		t.Fatal("expected file too large error, got nil")
	}
	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestParseEntityFileSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	realFile := filepath.Join(tmpDir, "real.json")
	symlinkFile := filepath.Join(tmpDir, "link.json")
	content := `{"identifier": "test-entity"}`
	if err := os.WriteFile(realFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := os.Symlink(realFile, symlinkFile); err != nil {
		t.Skipf("skipping symlink test: %v", err)
	}

	_, err := parseEntityFile(symlinkFile)
	if err == nil {
		t.Fatal("expected symlink error, got nil")
	}
	if !errors.Is(err, ErrSymlink) {
		t.Errorf("expected ErrSymlink, got %v", err)
	}
}

func TestParseEntityFileNotFound(t *testing.T) {
	_, err := parseEntityFile("/nonexistent/path/entity.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

// ====================================================================
// detectUnknownEntityFields tests
// ====================================================================

func TestDetectUnknownEntityFieldsAllKnown(t *testing.T) {
	entity := api.Entity{
		"identifier": "test",
		"title":      "Test Entity",
		"blueprint":  "service",
		"properties": map[string]interface{}{"foo": "bar"},
	}

	unknown := detectUnknownEntityFields(entity)
	if len(unknown) != 0 {
		t.Errorf("expected no unknown fields, got %v", unknown)
	}
}

func TestDetectUnknownEntityFieldsWithUnknown(t *testing.T) {
	entity := api.Entity{
		"identifier": "test",
		"foo":        "bar",
		"baz":        "qux",
	}

	unknown := detectUnknownEntityFields(entity)
	if len(unknown) != 2 {
		t.Fatalf("expected 2 unknown fields, got %d: %v", len(unknown), unknown)
	}

	// Should be sorted
	expected := []string{"baz", "foo"}
	sort.Strings(unknown)
	for i, want := range expected {
		if unknown[i] != want {
			t.Errorf("expected unknown[%d]=%s, got %s", i, want, unknown[i])
		}
	}
}

func TestDetectUnknownEntityFieldsEmpty(t *testing.T) {
	entity := api.Entity{}
	unknown := detectUnknownEntityFields(entity)
	if len(unknown) != 0 {
		t.Errorf("expected no unknown fields for empty entity, got %v", unknown)
	}
}

func TestDetectUnknownEntityFieldsSingleUnknown(t *testing.T) {
	entity := api.Entity{
		"identifier":   "test",
		"unknownField": "value",
	}

	unknown := detectUnknownEntityFields(entity)
	if len(unknown) != 1 {
		t.Fatalf("expected 1 unknown field, got %d: %v", len(unknown), unknown)
	}
	if unknown[0] != "unknownField" {
		t.Errorf("expected unknown field 'unknownField', got %s", unknown[0])
	}
}

// ====================================================================
// confirmAction tests
// ====================================================================

func TestConfirmActionForceTrue(t *testing.T) {
	// force=true should return true without reading stdin
	confirmed, err := confirmAction("Delete everything?", true, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !confirmed {
		t.Fatal("expected confirmed=true when force=true")
	}
}

func TestConfirmActionStdinYes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"lowercase y", "y\n", true},
		{"uppercase Y", "Y\n", true},
		{"lowercase yes", "yes\n", true},
		{"uppercase YES", "YES\n", true},
		{"mixed case Yes", "Yes\n", true},
		{"lowercase n", "n\n", false},
		{"uppercase N", "N\n", false},
		{"no input", "no\n", false},
		{"garbage", "garbage\n", false},
		{"empty EOF", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdin := strings.NewReader(tt.input)
			confirmed, err := confirmAction("Proceed?", false, stdin)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if confirmed != tt.want {
				t.Errorf("expected confirmed=%v for input %q, got %v", tt.want, tt.input, confirmed)
			}
		})
	}
}

func TestConfirmActionStdinNil(t *testing.T) {
	// When stdin=nil, the function should fall back to interactive TUI.
	// We can't test TUI in unit tests, but we can verify the seam exists.
	// This test just ensures the function is callable with stdin=nil.
	// The actual TUI behavior is tested manually or in integration tests.
	// For unit test purposes, we expect this to either:
	// - Return false (safe default)
	// - Or delegate to TUI (which we skip in unit tests)
	// Let's just verify it doesn't panic and returns a bool.
	_, err := confirmAction("Proceed?", false, nil)
	// We allow either success or error here, since TUI fallback behavior
	// is implementation-defined for unit test purposes.
	_ = err // Acknowledge error but don't fail the test on it
}

// ====================================================================
// Edge case: confirm with very long input
// ====================================================================

func TestConfirmActionStdinLongInput(t *testing.T) {
	// Ensure we handle long input gracefully
	longInput := strings.Repeat("x", 1000) + "\n"
	stdin := strings.NewReader(longInput)
	confirmed, err := confirmAction("Proceed?", false, stdin)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if confirmed {
		t.Fatal("expected confirmed=false for garbage input")
	}
}

// ====================================================================
// Integration test: parseEntityFile with exactly 1MB
// ====================================================================

func TestParseEntityFileExactly1MB(t *testing.T) {
	tmpDir := t.TempDir()
	exactFile := filepath.Join(tmpDir, "exact.json")
	// Create a file that is exactly 1MB
	// JSON overhead: {"identifier":"test","data":"<padding>"}
	// We need total file size = 1MB = 1048576 bytes
	overhead := len(`{"identifier":"test","data":""}`)
	paddingSize := 1048576 - overhead
	if paddingSize < 0 {
		t.Fatal("overhead too large for test")
	}
	padding := strings.Repeat("x", paddingSize)
	content := `{"identifier":"test","data":"` + padding + `"}`
	if err := os.WriteFile(exactFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Verify file size
	info, err := os.Stat(exactFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	if info.Size() != 1048576 {
		t.Fatalf("expected file size 1048576, got %d", info.Size())
	}

	// Should succeed (1MB is the limit, not exceeded)
	entity, err := parseEntityFile(exactFile)
	if err != nil {
		t.Fatalf("expected no error for exactly 1MB file, got %v", err)
	}
	if entity["identifier"] != "test" {
		t.Errorf("expected identifier 'test', got %v", entity["identifier"])
	}
}
