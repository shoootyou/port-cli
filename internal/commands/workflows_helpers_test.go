/**
 * @spec-handoff
 *
 * @interface validateWorkflowIdentifier(identifier string) error
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
 * @interface parseWorkflowFile(path string) (body map[string]interface{}, identifier string, err error)
 * @behavior
 *   - Auto-detects format by file extension: .json, .yaml, .yml
 *   - Rejects symlinks (use os.Lstat, check Mode&os.ModeSymlink)
 *   - Rejects files > 1MB (check size before reading via os.Stat)
 *   - Parses valid JSON/YAML into map[string]interface{}
 *   - Extracts top-level "identifier" field from parsed body and returns it as second return value
 *   - Returns error if "identifier" field is missing or not a string
 *   - Returns error for unknown extensions
 *   - Returns error for malformed JSON/YAML
 *   - Returns error for nonexistent files
 *   - Error messages MUST NOT leak full file paths (use filepath.Base or generic message)
 * @edge-cases
 *   - .json extension → json.Unmarshal
 *   - .yaml extension → yaml.Unmarshal
 *   - .yml extension → yaml.Unmarshal
 *   - .txt extension → ErrUnsupportedFormat
 *   - File exactly 1MB → ok, file 1MB+1 byte → ErrFileTooLarge
 *   - Symlink → ErrSymlink (before size check)
 *   - Malformed JSON → parse error (no path leak)
 *   - Malformed YAML → parse error (no path leak)
 *   - Missing "identifier" field → error
 *   - "identifier" field present but not string → error
 * @sentinel-errors
 *   - var ErrFileTooLarge = errors.New("file exceeds 1MB limit")
 *   - var ErrSymlink = errors.New("symlinks are not supported")
 *   - var ErrUnsupportedFormat = errors.New("unsupported file format")
 *   - var ErrMissingIdentifier = errors.New("missing required field: identifier")
 *
 * @interface confirmAction(prompt string, force bool, stdin io.Reader) (bool, error)
 * @behavior
 *   - If force==true → return (true, nil) immediately without reading stdin
 *   - If stdin != nil → read one line from stdin: "y"/"Y"/"yes"/"YES" (case-insensitive, trimmed) → true, anything else → false
 *   - Only "y" or "yes" (case-insensitive) count as confirmation
 *   - Any other input (including "n", "", EOF, garbage) → false
 * @edge-cases
 *   - force=true → (true, nil), no stdin read
 *   - stdin="y\n" → (true, nil)
 *   - stdin="yes\n" → (true, nil)
 *   - stdin="Y\n" → (true, nil)
 *   - stdin="YES\n" → (true, nil)
 *   - stdin="n\n" → (false, nil)
 *   - stdin="" (EOF) → (false, nil)
 *   - stdin="garbage\n" → (false, nil)
 *
 * @see internal/commands/helpers.go (entities pattern reference on feat/entities-crud branch)
 */

package commands

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ====================================================================
// validateWorkflowIdentifier tests
// ====================================================================

func TestValidateWorkflowIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		wantErr    error
	}{
		{
			name:       "valid identifier with dashes",
			identifier: "valid-workflow-id",
			wantErr:    nil,
		},
		{
			name:       "valid identifier with underscores",
			identifier: "valid_workflow_id",
			wantErr:    nil,
		},
		{
			name:       "valid alphanumeric identifier",
			identifier: "workflow123",
			wantErr:    nil,
		},
		{
			name:       "empty identifier",
			identifier: "",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "identifier with single slash",
			identifier: "/",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "identifier with slash in middle",
			identifier: "invalid/workflow",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "identifier with leading slash",
			identifier: "/workflow",
			wantErr:    ErrInvalidIdentifier,
		},
		{
			name:       "identifier with trailing slash",
			identifier: "workflow/",
			wantErr:    ErrInvalidIdentifier,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWorkflowIdentifier(tt.identifier)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

// ====================================================================
// parseWorkflowFile tests
// ====================================================================

func TestParseWorkflowFileValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "workflow.json")
	content := `{"identifier":"test-workflow","trigger":{"type":"manual"}}`
	if err := os.WriteFile(jsonFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	body, identifier, err := parseWorkflowFile(jsonFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if identifier != "test-workflow" {
		t.Errorf("expected identifier='test-workflow', got %q", identifier)
	}
	if body["identifier"] != "test-workflow" {
		t.Errorf("expected body[identifier]='test-workflow', got %v", body["identifier"])
	}
	if body["trigger"] == nil {
		t.Error("expected trigger field in body")
	}
}

func TestParseWorkflowFileValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "workflow.yaml")
	content := `identifier: test-workflow
trigger:
  type: manual
`
	if err := os.WriteFile(yamlFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	body, identifier, err := parseWorkflowFile(yamlFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if identifier != "test-workflow" {
		t.Errorf("expected identifier='test-workflow', got %q", identifier)
	}
	if body["identifier"] != "test-workflow" {
		t.Errorf("expected body[identifier]='test-workflow', got %v", body["identifier"])
	}
}

func TestParseWorkflowFileValidYML(t *testing.T) {
	tmpDir := t.TempDir()
	ymlFile := filepath.Join(tmpDir, "workflow.yml")
	content := `identifier: test-workflow
trigger:
  type: manual
`
	if err := os.WriteFile(ymlFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	body, identifier, err := parseWorkflowFile(ymlFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if identifier != "test-workflow" {
		t.Errorf("expected identifier='test-workflow', got %q", identifier)
	}
	if body["identifier"] != "test-workflow" {
		t.Errorf("expected body[identifier]='test-workflow', got %v", body["identifier"])
	}
}

func TestParseWorkflowFileMalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "bad.json")
	content := `{"identifier": "test", "trigger": {invalid}}`
	if err := os.WriteFile(jsonFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseWorkflowFile(jsonFile)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	// Verify error does NOT leak full path
	if strings.Contains(err.Error(), tmpDir) {
		t.Errorf("error message leaks full path: %v", err)
	}
}

func TestParseWorkflowFileMalformedYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "bad.yaml")
	content := `identifier: test
trigger:
  - invalid: [unclosed
`
	if err := os.WriteFile(yamlFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseWorkflowFile(yamlFile)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
	// Verify error does NOT leak full path
	if strings.Contains(err.Error(), tmpDir) {
		t.Errorf("error message leaks full path: %v", err)
	}
}

func TestParseWorkflowFileUnsupportedExtension(t *testing.T) {
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "workflow.txt")
	content := `identifier: test`
	if err := os.WriteFile(txtFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseWorkflowFile(txtFile)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}

func TestParseWorkflowFileExceedsSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.json")

	// Create a file > 1MB (1048576 bytes)
	// JSON overhead: {"identifier":"test","data":"<padding>"}
	overhead := len(`{"identifier":"test","data":""}`)
	paddingSize := 1048577 - overhead // 1MB + 1 byte
	padding := strings.Repeat("x", paddingSize)
	content := `{"identifier":"test","data":"` + padding + `"}`

	if err := os.WriteFile(largeFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseWorkflowFile(largeFile)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestParseWorkflowFileExactly1MB(t *testing.T) {
	tmpDir := t.TempDir()
	exactFile := filepath.Join(tmpDir, "exact.json")

	// Create a file that is exactly 1MB (1048576 bytes)
	overhead := len(`{"identifier":"test","data":""}`)
	paddingSize := 1048576 - overhead
	if paddingSize < 0 {
		t.Fatal("overhead too large for test")
	}
	padding := strings.Repeat("x", paddingSize)
	content := `{"identifier":"test","data":"` + padding + `"}`

	if err := os.WriteFile(exactFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Verify file is exactly 1MB
	stat, err := os.Stat(exactFile)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() != 1048576 {
		t.Fatalf("expected file size 1048576, got %d", stat.Size())
	}

	// Should succeed
	body, identifier, err := parseWorkflowFile(exactFile)
	if err != nil {
		t.Fatalf("expected no error for exactly 1MB file, got %v", err)
	}
	if identifier != "test" {
		t.Errorf("expected identifier='test', got %q", identifier)
	}
	if body["identifier"] != "test" {
		t.Error("expected body to parse correctly")
	}
}

func TestParseWorkflowFileSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target.json")
	symlinkFile := filepath.Join(tmpDir, "link.json")

	content := `{"identifier":"test","trigger":{"type":"manual"}}`
	if err := os.WriteFile(targetFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(targetFile, symlinkFile); err != nil {
		t.Skip("symlink creation not supported on this platform")
	}

	_, _, err := parseWorkflowFile(symlinkFile)
	if !errors.Is(err, ErrSymlink) {
		t.Errorf("expected ErrSymlink, got %v", err)
	}
}

func TestParseWorkflowFileNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	nonexistentFile := filepath.Join(tmpDir, "does-not-exist.json")

	_, _, err := parseWorkflowFile(nonexistentFile)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	// Verify error does NOT leak full path
	if strings.Contains(err.Error(), tmpDir) {
		t.Errorf("error message leaks full path: %v", err)
	}
}

func TestParseWorkflowFileMissingIdentifier(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "no-id.json")
	content := `{"trigger":{"type":"manual"}}`
	if err := os.WriteFile(jsonFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseWorkflowFile(jsonFile)
	if !errors.Is(err, ErrMissingIdentifier) {
		t.Errorf("expected ErrMissingIdentifier, got %v", err)
	}
}

func TestParseWorkflowFileIdentifierNotString(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "bad-id.json")
	content := `{"identifier":123,"trigger":{"type":"manual"}}`
	if err := os.WriteFile(jsonFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseWorkflowFile(jsonFile)
	if err == nil {
		t.Fatal("expected error when identifier is not a string, got nil")
	}
	// Should be ErrMissingIdentifier or similar
	if !errors.Is(err, ErrMissingIdentifier) {
		t.Logf("got error (acceptable if it mentions identifier type): %v", err)
	}
}

// ====================================================================
// confirmAction tests
// ====================================================================

func TestConfirmActionForceTrue(t *testing.T) {
	// force=true should return true without reading stdin
	confirmed, err := confirmAction("Delete workflow?", true, nil)
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
		{"whitespace only", "  \n", false},
		{"y with leading whitespace", "  y\n", true},
		{"yes with trailing whitespace", "yes  \n", true},
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

func TestConfirmActionStdinMultipleReads(t *testing.T) {
	// Ensure confirmAction only reads once and doesn't block on subsequent reads
	stdin := strings.NewReader("y\n")
	confirmed, err := confirmAction("First?", false, stdin)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !confirmed {
		t.Fatal("expected confirmed=true")
	}

	// Second call with exhausted reader should return false (EOF)
	confirmed2, err := confirmAction("Second?", false, stdin)
	if err != nil && err != io.EOF {
		t.Fatalf("expected EOF or no error, got %v", err)
	}
	if confirmed2 {
		t.Error("expected confirmed=false on exhausted reader")
	}
}
