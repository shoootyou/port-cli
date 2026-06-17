package commands

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Sentinel errors
var (
	ErrInvalidIdentifier = errors.New("invalid identifier")
	ErrFileTooLarge      = errors.New("file exceeds 1MB limit")
	ErrSymlink           = errors.New("symlinks are not supported")
	ErrUnsupportedFormat = errors.New("unsupported file format")
	ErrMissingIdentifier = errors.New("missing required field: identifier")
)

// validateWorkflowIdentifier validates that the identifier does not contain '/' and is not empty.
func validateWorkflowIdentifier(identifier string) error {
	if identifier == "" {
		return ErrInvalidIdentifier
	}
	if strings.Contains(identifier, "/") {
		return ErrInvalidIdentifier
	}
	return nil
}

// parseWorkflowFile parses a workflow file (JSON or YAML) and extracts the workflow body
// and identifier. Returns an error if the file is a symlink, exceeds 1MB, has an unsupported
// format, is malformed, or is missing a valid identifier field.
func parseWorkflowFile(path string) (map[string]interface{}, string, error) {
	// Check for symlink using Lstat (does not follow symlinks)
	lstat, err := os.Lstat(path)
	if err != nil {
		// Don't leak full path - use generic error message
		return nil, "", fmt.Errorf("cannot read file %q", filepath.Base(path))
	}
	if lstat.Mode()&os.ModeSymlink != 0 {
		return nil, "", ErrSymlink
	}

	// Check file size using Stat (follows symlinks if any, but we've already rejected them)
	stat, err := os.Stat(path)
	if err != nil {
		// Don't leak full path - use generic error message
		return nil, "", fmt.Errorf("cannot stat file %q", filepath.Base(path))
	}
	const maxSize = 1048576 // 1MB exactly
	if stat.Size() > maxSize {
		return nil, "", ErrFileTooLarge
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		// Don't leak full path - use generic error message
		return nil, "", fmt.Errorf("cannot read file %q", filepath.Base(path))
	}

	// Detect format by extension
	ext := strings.ToLower(filepath.Ext(path))
	var body map[string]interface{}

	switch ext {
	case ".json":
		if err := json.Unmarshal(content, &body); err != nil {
			// Don't leak full path - generic error message only
			return nil, "", fmt.Errorf("failed to parse JSON in %q", filepath.Base(path))
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(content, &body); err != nil {
			// Don't leak full path - generic error message only
			return nil, "", fmt.Errorf("failed to parse YAML in %q", filepath.Base(path))
		}
	default:
		return nil, "", ErrUnsupportedFormat
	}

	// Extract identifier field
	identifierRaw, ok := body["identifier"]
	if !ok {
		return nil, "", ErrMissingIdentifier
	}
	identifier, ok := identifierRaw.(string)
	if !ok {
		return nil, "", ErrMissingIdentifier
	}

	return body, identifier, nil
}

// confirmAction prompts the user for confirmation. If force is true, returns true immediately
// without reading stdin. Otherwise, reads one line from stdin and returns true only if the
// input is "y" or "yes" (case-insensitive, trimmed).
func confirmAction(prompt string, force bool, stdin io.Reader) (bool, error) {
	if force {
		return true, nil
	}

	// Print prompt to stderr (so it doesn't interfere with stdout)
	fmt.Fprintln(os.Stderr, prompt)

	// Read one line from stdin
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		// EOF or error
		if err := scanner.Err(); err != nil {
			return false, err
		}
		// EOF without error
		return false, nil
	}

	line := strings.TrimSpace(scanner.Text())
	lineLower := strings.ToLower(line)
	return lineLower == "y" || lineLower == "yes", nil
}
