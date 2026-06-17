package commands

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/huh/v2"
	"github.com/port-experimental/port-cli/internal/api"
	"github.com/port-experimental/port-cli/internal/styles"
	"gopkg.in/yaml.v3"
)

// Sentinel errors
var (
	ErrInvalidIdentifier = errors.New("invalid identifier")
	ErrFileTooLarge      = errors.New("file exceeds 1MB limit")
	ErrSymlink           = errors.New("symlinks are not supported")
	ErrUnsupportedFormat = errors.New("unsupported file format")
)

const maxFileSize = 1048576 // 1MB

// validateEntityIdentifier checks if an identifier is valid for Port entities.
// Returns ErrInvalidIdentifier if the identifier is empty or contains '/'.
func validateEntityIdentifier(identifier string) error {
	if identifier == "" {
		return ErrInvalidIdentifier
	}
	if strings.Contains(identifier, "/") {
		return ErrInvalidIdentifier
	}
	return nil
}

// parseEntityFile reads and parses a JSON or YAML file into an api.Entity.
// It rejects symlinks, files larger than 1MB, and unsupported file formats.
func parseEntityFile(path string) (api.Entity, error) {
	// Check for symlink first using Lstat
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlink
	}

	// Check file size before reading
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fileInfo.Size() > maxFileSize {
		return nil, ErrFileTooLarge
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Detect format by extension
	ext := strings.ToLower(filepath.Ext(path))
	var entity api.Entity

	switch ext {
	case ".json":
		if err := json.Unmarshal(content, &entity); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(content, &entity); err != nil {
			return nil, fmt.Errorf("failed to parse YAML: %w", err)
		}
	default:
		return nil, ErrUnsupportedFormat
	}

	return entity, nil
}

// detectUnknownEntityFields returns a sorted list of top-level entity fields
// that are not part of the known Port entity schema.
func detectUnknownEntityFields(entity api.Entity) []string {
	knownFields := map[string]bool{
		"identifier": true,
		"title":      true,
		"blueprint":  true,
		"properties": true,
		"relations":  true,
		"team":       true,
		"icon":       true,
		"createdAt":  true,
		"createdBy":  true,
		"updatedAt":  true,
		"updatedBy":  true,
	}

	unknown := make([]string, 0)
	for key := range entity {
		if !knownFields[key] {
			unknown = append(unknown, key)
		}
	}

	sort.Strings(unknown)
	return unknown
}

// confirmAction prompts the user for confirmation with a yes/no question.
// It supports three modes:
// - If force is true, returns (true, nil) immediately without prompting
// - If stdin is not nil, reads from stdin: "y"/"yes" (case-insensitive) → true, else → false
// - If stdin is nil, falls back to interactive TUI prompt using huh
func confirmAction(prompt string, force bool, stdin io.Reader) (bool, error) {
	// Fast path: force=true skips all prompting
	if force {
		return true, nil
	}

	// If stdin is provided, read from it (for testing or piped input)
	if stdin != nil {
		scanner := bufio.NewScanner(stdin)
		if scanner.Scan() {
			response := strings.TrimSpace(strings.ToLower(scanner.Text()))
			return response == "y" || response == "yes", nil
		}
		// EOF or error → treat as false (no confirmation)
		return false, nil
	}

	// Fall back to interactive TUI
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(prompt).
				Value(&confirmed),
		),
	).WithTheme(&styles.FormTheme{})

	if err := form.Run(); err != nil {
		return false, err
	}

	return confirmed, nil
}
