package catalog

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxFileSize = 1 << 20 // 1 MB

// identifierRe is the allowed character set for skill identifiers.
// Only letters, digits, hyphens, underscores, and dots are permitted.
// Slashes, backslashes, and other special characters are rejected to prevent
// path-traversal attacks when the identifier is used in API URL paths.
var identifierRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// frontmatterFields is the set of fields we unmarshal from the YAML frontmatter.
type frontmatterFields struct {
	Identifier  string `yaml:"identifier"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Location    string `yaml:"location"`
}

// ParseSkillFile reads a Markdown file with YAML frontmatter and returns a
// SkillEntity. The file must not be a symlink and must not exceed 1 MB.
//
// Format:
//
//	---
//	identifier: my-skill
//	title: My Skill        (optional → defaults to identifier)
//	description: …         (required)
//	location: global       (optional → defaults to "global"; "project" also accepted)
//	---
//	Markdown body (becomes Instructions after strings.TrimSpace).
func ParseSkillFile(path string) (SkillEntity, error) {
	// Symlink guard — use Lstat so we inspect the link itself, not its target.
	linfo, err := os.Lstat(path)
	if err != nil {
		return SkillEntity{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		return SkillEntity{}, fmt.Errorf("symlink not allowed: %s", path)
	}

	// Size guard.
	if linfo.Size() > maxFileSize {
		return SkillEntity{}, fmt.Errorf("file exceeds 1 MB limit (%d bytes): %s", linfo.Size(), path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return SkillEntity{}, fmt.Errorf("read %s: %w", path, err)
	}

	// Normalise CRLF → LF.
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	content := string(data)

	// Split frontmatter from body. The file must start with "---\n". We then
	// scan for the closing "---\n" (or "---" at end of file).
	if !strings.HasPrefix(content, "---\n") {
		return SkillEntity{}, fmt.Errorf("file does not start with YAML frontmatter (---): %s", path)
	}

	// Strip the leading "---\n" fence and look for the closing delimiter.
	rest := content[4:] // skip "---\n"

	closingIdx := strings.Index(rest, "\n---\n")
	var frontmatterStr, body string
	if closingIdx >= 0 {
		frontmatterStr = rest[:closingIdx]
		body = rest[closingIdx+5:] // skip "\n---\n"
	} else if strings.HasSuffix(rest, "\n---") {
		// Edge case: closing delimiter at end of file without trailing newline.
		closingIdx = len(rest) - 4
		frontmatterStr = rest[:closingIdx]
		body = ""
	} else {
		return SkillEntity{}, fmt.Errorf("missing closing frontmatter delimiter (---) in: %s", path)
	}

	// Parse YAML frontmatter.
	var fm frontmatterFields
	if err := yaml.Unmarshal([]byte(frontmatterStr), &fm); err != nil {
		return SkillEntity{}, fmt.Errorf("invalid YAML frontmatter in %s: %w", path, err)
	}

	// Validate required fields.
	if fm.Identifier == "" {
		return SkillEntity{}, fmt.Errorf("missing required frontmatter field: identifier")
	}
	if !identifierRe.MatchString(fm.Identifier) {
		return SkillEntity{}, fmt.Errorf("invalid identifier %q: must match [A-Za-z0-9._-]", fm.Identifier)
	}
	if fm.Description == "" {
		return SkillEntity{}, fmt.Errorf("missing required frontmatter field: description")
	}

	// Validate and default optional fields.
	switch fm.Location {
	case "", LocationGlobal, LocationProject:
		// valid
	default:
		return SkillEntity{}, fmt.Errorf("invalid location %q: must be \"global\" or \"project\"", fm.Location)
	}

	title := fm.Title
	if title == "" {
		title = fm.Identifier
	}

	location := fm.Location
	if location == "" {
		location = LocationGlobal
	}

	instructions := strings.TrimSpace(body)

	return SkillEntity{
		Identifier:   fm.Identifier,
		Title:        title,
		Description:  fm.Description,
		Location:     location,
		Instructions: instructions,
	}, nil
}
