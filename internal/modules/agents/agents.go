package agents

import "github.com/port-experimental/port-cli/internal/api"

const agentBlueprint = "_ai_agent"

var promptPropertyCandidates = []string{
	"prompt",
	"system_prompt",
	"systemPrompt",
	"instructions",
}

// AgentEntity is the structured representation of a Port AI agent entity.
type AgentEntity struct {
	Identifier string
	Title      string
	Blueprint  string
	CreatedAt  string
	UpdatedAt  string
	Properties map[string]interface{}
}

// ListOptions controls the List operation.
type ListOptions struct{}

// ListResult holds the output of a List call.
type ListResult struct {
	Entities []AgentEntity
}

// GetOptions controls the Get operation.
type GetOptions struct {
	AgentID string
}

// GetResult holds the output of a Get call.
type GetResult struct {
	Entity AgentEntity
}

// UpdateOptions controls the Update operation.
type UpdateOptions struct {
	AgentID   string
	NewPrompt string
}

// UpdateResult holds the output of an Update call.
type UpdateResult struct {
	Entity AgentEntity
}

// parseAgentEntity converts an untyped api.Entity map into AgentEntity.
// Rules:
//   - "identifier", "title", "blueprint", "createdAt", "updatedAt" → string (zero if missing or wrong type)
//   - "properties" → map[string]interface{} (empty map if missing or wrong type — NEVER nil)
func parseAgentEntity(raw api.Entity) AgentEntity {
	str := func(key string) string {
		v, _ := raw[key].(string)
		return v
	}

	props, ok := raw["properties"].(map[string]interface{})
	if !ok {
		props = map[string]interface{}{}
	}

	return AgentEntity{
		Identifier: str("identifier"),
		Title:      str("title"),
		Blueprint:  str("blueprint"),
		CreatedAt:  str("createdAt"),
		UpdatedAt:  str("updatedAt"),
		Properties: props,
	}
}
