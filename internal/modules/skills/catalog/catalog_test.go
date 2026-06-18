// Tests for SkillEntity JSON serialisation (finding B).
//
// SkillEntity must marshal to Port-style lowercase JSON keys so that
// `port skills catalog ... -o json` output is machine-readable and matches
// the Port API wire format.  Currently the struct carries no JSON tags
// (fields are PascalCase) so these tests are RED until Kou adds the tags.

package catalog_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/port-experimental/port-cli/internal/modules/skills/catalog"
)

// ---------------------------------------------------------------------------
// B — SkillEntity JSON output uses Port-style lowercase keys
// ---------------------------------------------------------------------------

func TestSkillEntity_JSONMarshal_UsesLowercaseKeys(t *testing.T) {
	entity := catalog.SkillEntity{
		Identifier:   "x",
		Title:        "T",
		Description:  "d",
		Location:     "global",
		Instructions: "i",
	}

	data, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("json.Marshal(SkillEntity) returned unexpected error: %v", err)
	}
	raw := string(data)

	// Must contain lowercase keys.
	for _, want := range []string{`"identifier"`, `"title"`, `"description"`, `"location"`, `"instructions"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("JSON output missing lowercase key %s\nfull JSON: %s", want, raw)
		}
	}

	// Must NOT contain PascalCase keys — those are the pre-fix form.
	for _, bad := range []string{`"Identifier"`, `"Title"`, `"Description"`, `"Location"`, `"Instructions"`} {
		if strings.Contains(raw, bad) {
			t.Errorf("JSON output contains PascalCase key %s (want lowercase)\nfull JSON: %s", bad, raw)
		}
	}
}

// Verify round-trip: unmarshal the marshalled bytes back into a map and check values.
func TestSkillEntity_JSONMarshal_ValuesPreserved(t *testing.T) {
	entity := catalog.SkillEntity{
		Identifier:   "storage-account-sbs",
		Title:        "Storage Account SBS",
		Description:  "Compliance skill for SBS",
		Location:     "global",
		Instructions: "Follow the rules.",
	}

	data, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	checks := map[string]string{
		"identifier":   "storage-account-sbs",
		"title":        "Storage Account SBS",
		"description":  "Compliance skill for SBS",
		"location":     "global",
		"instructions": "Follow the rules.",
	}
	for key, wantVal := range checks {
		if got[key] != wantVal {
			t.Errorf("key %q: got %q, want %q", key, got[key], wantVal)
		}
	}
}
