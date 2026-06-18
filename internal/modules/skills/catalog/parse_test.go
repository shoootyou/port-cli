// @spec-handoff
// @interface ParseSkillFile(path string) (SkillEntity, error)
//
// @interface SkillEntity struct {
//     Identifier   string
//     Title        string   // defaults to Identifier when empty
//     Description  string
//     Location     string   // "global" (default) | "project"
//     Instructions string   // Markdown body, verbatim after strings.TrimSpace
// }
//
// @behavior
//   - Reads a Markdown file with YAML frontmatter delimited by "---\n".
//   - Parses frontmatter fields: identifier, title, description, location.
//   - Everything after the second "---\n" delimiter is the body (Instructions).
//   - Instructions is set to strings.TrimSpace(body) — leading/trailing blank
//     lines stripped, interior content byte-equal.
//   - When title is absent or empty in frontmatter, Title defaults to Identifier.
//   - When location is absent or empty, Location defaults to "global".
//   - "location: project" is accepted; stored as "project".
//   - CRLF input (\r\n) is normalised to LF (\n) before parsing, so files
//     authored on Windows are accepted without error.
//   - Unicode content (Spanish accents, ⚠️, →, —) round-trips byte-equal through
//     the Instructions field.
//
// @edge-cases
//   - Missing identifier  → error containing "identifier"
//   - Missing description → error containing "description"
//   - Invalid location (e.g. "datacenter") → error containing "location"
//   - File > 1 MB         → error (size guard)
//   - Symlink path        → error (symlink guard)
//   - Non-existent path   → error (os.Stat / os.Open failure)
//
// @see internal/api/requests.go — Entity type for downstream usage
// @see internal/modules/skills/catalog/create_test.go — CreateSkill contract

package catalog_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/port-experimental/port-cli/internal/modules/skills/catalog"
)

// writeFile is a helper that creates a named file in dir with the given content.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Happy-path table tests
// ---------------------------------------------------------------------------

func TestParseSkillFile_HappyPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  catalog.SkillEntity
	}{
		{
			name:  "full frontmatter with body",
			input: "---\nidentifier: my-skill\ntitle: My Skill\ndescription: Does things\nlocation: global\n---\n\n# Instructions\n\nDo the thing.\n",
			want: catalog.SkillEntity{
				Identifier:   "my-skill",
				Title:        "My Skill",
				Description:  "Does things",
				Location:     "global",
				Instructions: "# Instructions\n\nDo the thing.",
			},
		},
		{
			name:  "title absent defaults to identifier",
			input: "---\nidentifier: no-title-skill\ndescription: A skill without title\nlocation: global\n---\nInstructions here.\n",
			want: catalog.SkillEntity{
				Identifier:   "no-title-skill",
				Title:        "no-title-skill",
				Description:  "A skill without title",
				Location:     "global",
				Instructions: "Instructions here.",
			},
		},
		{
			name:  "location absent defaults to global",
			input: "---\nidentifier: no-loc-skill\ntitle: No Loc\ndescription: No location field\n---\nBody text.\n",
			want: catalog.SkillEntity{
				Identifier:   "no-loc-skill",
				Title:        "No Loc",
				Description:  "No location field",
				Location:     "global",
				Instructions: "Body text.",
			},
		},
		{
			name:  "location project accepted",
			input: "---\nidentifier: proj-skill\ntitle: Proj Skill\ndescription: Project scoped\nlocation: project\n---\nProject instructions.\n",
			want: catalog.SkillEntity{
				Identifier:   "proj-skill",
				Title:        "Proj Skill",
				Description:  "Project scoped",
				Location:     "project",
				Instructions: "Project instructions.",
			},
		},
		{
			name:  "leading blank lines in body trimmed, interior preserved",
			input: "---\nidentifier: blank-lines\ntitle: Blank Lines\ndescription: Leading blanks\nlocation: global\n---\n\n\n\nActual content.\n\nMore content.\n",
			want: catalog.SkillEntity{
				Identifier:   "blank-lines",
				Title:        "Blank Lines",
				Description:  "Leading blanks",
				Location:     "global",
				Instructions: "Actual content.\n\nMore content.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "skill.md", tt.input)

			got, err := catalog.ParseSkillFile(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Identifier != tt.want.Identifier {
				t.Errorf("Identifier: got %q, want %q", got.Identifier, tt.want.Identifier)
			}
			if got.Title != tt.want.Title {
				t.Errorf("Title: got %q, want %q", got.Title, tt.want.Title)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description: got %q, want %q", got.Description, tt.want.Description)
			}
			if got.Location != tt.want.Location {
				t.Errorf("Location: got %q, want %q", got.Location, tt.want.Location)
			}
			if got.Instructions != tt.want.Instructions {
				t.Errorf("Instructions mismatch:\ngot:  %q\nwant: %q", got.Instructions, tt.want.Instructions)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unicode round-trip — substrings from real skill files
// ---------------------------------------------------------------------------

// unicodeStorageAccountBody contains representative substrings from
// /code-projects/personal/modules-factory/port-agents/v3/skills/storage-account-sbs.md
// (Spanish accents ó/ñ/á, ⚠️, em-dash —).
const unicodeStorageAccountBody = `# Storage Account — Convenciones SBS Credicorp

La SBS exige retención de evidencia transaccional por **7 años** mínimo.

> ⚠️ ` + "`locked = true`" + ` no se puede revertir. Una vez locked, ni siquiera
> un owner del subscription puede borrar blobs hasta vencido el período.`

// unicodeNamingConventionBody contains representative substrings from
// /code-projects/personal/modules-factory/port-agents/v3/skills/naming-convention-credicorp.md
// (arrows →, accents, table notation).
const unicodeNamingConventionBody = `# Naming Convention — Credicorp Azure

Patrón general: ` + "`<tipo>-cc-<env>-<app>-<region>-<###>`" + `

| Campo  | Notas                                       |
|--------|---------------------------------------------|
| ` + "`env`" + `   | ` + "`dev`" + ` \| ` + "`qa`" + ` \| ` + "`uat`" + ` \| ` + "`prod`" + ` → sin abreviaturas |`

func TestParseSkillFile_UnicodeBodyRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		checkSubstr []string
	}{
		{
			name: "storage-account-sbs unicode",
			body: unicodeStorageAccountBody,
			checkSubstr: []string{
				"Storage Account — Convenciones SBS Credicorp",
				"retención",
				"mínimo",
				"⚠️",
				"período",
			},
		},
		{
			name: "naming-convention-credicorp unicode",
			body: unicodeNamingConventionBody,
			checkSubstr: []string{
				"Naming Convention — Credicorp Azure",
				"Patrón",
				"→",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := fmt.Sprintf(
				"---\nidentifier: unicode-skill\ntitle: Unicode Skill\ndescription: Unicode test\nlocation: global\n---\n%s\n",
				tt.body,
			)
			dir := t.TempDir()
			path := writeFile(t, dir, "unicode-skill.md", input)

			got, err := catalog.ParseSkillFile(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, sub := range tt.checkSubstr {
				if !strings.Contains(got.Instructions, sub) {
					t.Errorf("Instructions missing substring %q\nfull:\n%s", sub, got.Instructions)
				}
			}

			// Byte-equal check: trim the body the same way the implementation should
			wantInstructions := strings.TrimSpace(tt.body)
			if got.Instructions != wantInstructions {
				t.Errorf("Instructions not byte-equal:\ngot:  %q\nwant: %q", got.Instructions, wantInstructions)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CRLF normalisation
// ---------------------------------------------------------------------------

func TestParseSkillFile_CRLFNormalisedToLF(t *testing.T) {
	// The file uses \r\n line endings (Windows). The parser must normalise to \n.
	content := "---\r\nidentifier: crlf-skill\r\ntitle: CRLF Skill\r\ndescription: Windows line endings\r\nlocation: global\r\n---\r\nInstructions on Windows.\r\n"

	dir := t.TempDir()
	path := writeFile(t, dir, "crlf.md", content)

	got, err := catalog.ParseSkillFile(path)
	if err != nil {
		t.Fatalf("unexpected error on CRLF input: %v", err)
	}
	if got.Identifier != "crlf-skill" {
		t.Errorf("Identifier: got %q, want %q", got.Identifier, "crlf-skill")
	}
	// Instructions must not contain \r — CRLF normalised away.
	if strings.Contains(got.Instructions, "\r") {
		t.Errorf("Instructions still contain \\r after CRLF normalisation: %q", got.Instructions)
	}
	if got.Instructions != "Instructions on Windows." {
		t.Errorf("Instructions: got %q, want %q", got.Instructions, "Instructions on Windows.")
	}
}

// ---------------------------------------------------------------------------
// Rejection cases
// ---------------------------------------------------------------------------

func TestParseSkillFile_MissingIdentifierReturnsError(t *testing.T) {
	input := "---\ntitle: No ID\ndescription: Missing identifier field\nlocation: global\n---\nBody.\n"
	dir := t.TempDir()
	path := writeFile(t, dir, "no-id.md", input)

	_, err := catalog.ParseSkillFile(path)
	if err == nil {
		t.Fatal("expected error for missing identifier, got nil")
	}
	if !strings.Contains(err.Error(), "identifier") {
		t.Errorf("error should mention 'identifier', got: %v", err)
	}
}

func TestParseSkillFile_MissingDescriptionReturnsError(t *testing.T) {
	input := "---\nidentifier: no-desc\ntitle: No Desc\nlocation: global\n---\nBody.\n"
	dir := t.TempDir()
	path := writeFile(t, dir, "no-desc.md", input)

	_, err := catalog.ParseSkillFile(path)
	if err == nil {
		t.Fatal("expected error for missing description, got nil")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Errorf("error should mention 'description', got: %v", err)
	}
}

func TestParseSkillFile_InvalidLocationReturnsError(t *testing.T) {
	input := "---\nidentifier: bad-loc\ntitle: Bad Loc\ndescription: Invalid location\nlocation: datacenter\n---\nBody.\n"
	dir := t.TempDir()
	path := writeFile(t, dir, "bad-loc.md", input)

	_, err := catalog.ParseSkillFile(path)
	if err == nil {
		t.Fatal("expected error for invalid location, got nil")
	}
	if !strings.Contains(err.Error(), "location") {
		t.Errorf("error should mention 'location', got: %v", err)
	}
}

func TestParseSkillFile_FileTooLargeReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")

	// Write a file slightly over 1 MB (1<<20 = 1_048_576 bytes).
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create big file: %v", err)
	}
	// Write valid frontmatter header first, then pad with garbage.
	header := "---\nidentifier: big-skill\ntitle: Big\ndescription: Too big\nlocation: global\n---\n"
	if _, err := f.WriteString(header); err != nil {
		f.Close()
		t.Fatalf("write header: %v", err)
	}
	padding := make([]byte, (1<<20)+1)
	for i := range padding {
		padding[i] = 'x'
	}
	if _, err := f.Write(padding); err != nil {
		f.Close()
		t.Fatalf("write padding: %v", err)
	}
	f.Close()

	_, err = catalog.ParseSkillFile(path)
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
}

func TestParseSkillFile_SymlinkReturnsError(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, dir, "real.md", "---\nidentifier: real\ntitle: Real\ndescription: Real\nlocation: global\n---\nBody.\n")
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation failed (unsupported env): %v", err)
	}

	_, err := catalog.ParseSkillFile(link)
	if err == nil {
		t.Fatal("expected error for symlink path, got nil")
	}
}

func TestParseSkillFile_NonExistentPathReturnsError(t *testing.T) {
	_, err := catalog.ParseSkillFile("/nonexistent/path/to/skill.md")
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

// ---------------------------------------------------------------------------
// D — identifier path-traversal rejection in the parser
// ---------------------------------------------------------------------------

// TestParseSkillFile_PathTraversalIdentifierRejected asserts that identifiers
// containing path separators or traversal sequences are rejected with an error
// containing "identifier".
//
// The allowed character set is ^[A-Za-z0-9._-]+$ — only letters, digits,
// dots, underscores, and hyphens.  No slashes or backslashes.
//
// Currently no guard exists in parse.go so these tests are RED until Kou adds
// the validation.
func TestParseSkillFile_PathTraversalIdentifierRejected(t *testing.T) {
	traversalCases := []struct {
		name       string
		identifier string
	}{
		{
			name:       "unix path traversal with dotdot",
			identifier: "../other-bp/x",
		},
		{
			name:       "unix nested path",
			identifier: "a/b",
		},
		{
			name:       "windows backslash traversal",
			identifier: `..\\x`,
		},
		{
			name:       "leading slash",
			identifier: "/absolute",
		},
		{
			name:       "embedded slash",
			identifier: "some/skill",
		},
	}

	for _, tc := range traversalCases {
		t.Run(tc.name, func(t *testing.T) {
			// Write a valid file except for the identifier under test.
			input := fmt.Sprintf(
				"---\nidentifier: %s\ntitle: T\ndescription: desc\nlocation: global\n---\nBody.\n",
				tc.identifier,
			)
			dir := t.TempDir()
			path := writeFile(t, dir, "traversal.md", input)

			_, err := catalog.ParseSkillFile(path)
			if err == nil {
				t.Fatalf("expected error for traversal identifier %q, got nil", tc.identifier)
			}
			if !strings.Contains(err.Error(), "identifier") {
				t.Errorf("error should mention 'identifier', got: %v", err)
			}
		})
	}
}

// TestParseSkillFile_ValidIdentifierAccepted is the positive complement of the
// traversal rejection tests: identifiers matching ^[A-Za-z0-9._-]+$ must NOT
// be rejected.
func TestParseSkillFile_ValidIdentifierAccepted(t *testing.T) {
	validCases := []struct {
		name       string
		identifier string
	}{
		{
			name:       "letters digits hyphen",
			identifier: "storage-account-sbs",
		},
		{
			name:       "with dot",
			identifier: "my.skill.v2",
		},
		{
			name:       "with underscore",
			identifier: "my_skill",
		},
		{
			name:       "mixed",
			identifier: "Abc-123_x.y",
		},
	}

	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf(
				"---\nidentifier: %s\ntitle: T\ndescription: desc\nlocation: global\n---\nBody.\n",
				tc.identifier,
			)
			dir := t.TempDir()
			path := writeFile(t, dir, "valid.md", input)

			got, err := catalog.ParseSkillFile(path)
			if err != nil {
				t.Fatalf("unexpected error for valid identifier %q: %v", tc.identifier, err)
			}
			if got.Identifier != tc.identifier {
				t.Errorf("Identifier: got %q, want %q", got.Identifier, tc.identifier)
			}
		})
	}
}
