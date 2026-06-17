package catalog

import "github.com/port-experimental/port-cli/internal/api"

// BlueprintID is the Port blueprint identifier for skills.
const BlueprintID = "skill"

// Location constants for skill scope.
const (
	LocationGlobal  = "global"
	LocationProject = "project"
)

// SkillEntity represents a skill provisioned in the Port catalog.
type SkillEntity struct {
	Identifier   string
	Title        string // defaults to Identifier when empty
	Description  string
	Location     string // "global" (default) | "project"
	Instructions string // Markdown body, TrimSpace applied
}

// CreateOptions controls the write behaviour of CreateSkill.
type CreateOptions struct {
	Force bool // overwrite existing entity (merge=false)
	Patch bool // partial update via PATCH
}

// toAPIEntity converts a SkillEntity to the api.Entity wire format.
func toAPIEntity(e SkillEntity) api.Entity {
	title := e.Title
	if title == "" {
		title = e.Identifier
	}

	location := e.Location
	if location == "" {
		location = LocationGlobal
	}

	return api.Entity{
		"identifier": e.Identifier,
		"title":      title,
		"blueprint":  BlueprintID,
		"properties": map[string]interface{}{
			"description":  e.Description,
			"instructions": e.Instructions,
			"location":     location,
		},
	}
}

// parseSkillEntity converts a raw api.Entity returned by the Port API back into
// a SkillEntity. Used by ListSkills and GetSkill.
func parseSkillEntity(e api.Entity) SkillEntity {
	identifier, _ := e["identifier"].(string)
	title, _ := e["title"].(string)
	if title == "" {
		title = identifier
	}

	var description, instructions, location string
	if props, ok := e["properties"].(map[string]interface{}); ok {
		description, _ = props["description"].(string)
		instructions, _ = props["instructions"].(string)
		location, _ = props["location"].(string)
	}
	if location == "" {
		location = LocationGlobal
	}

	return SkillEntity{
		Identifier:   identifier,
		Title:        title,
		Description:  description,
		Location:     location,
		Instructions: instructions,
	}
}
