package catalog

import (
	"context"
	"strings"

	"github.com/port-experimental/port-cli/internal/api"
)

// skillBlueprint is the canonical Port blueprint schema for skills.
// Source: https://docs.port.io/ai-interfaces/skills/
var skillBlueprint = api.Blueprint{
	"identifier": BlueprintID,
	"title":      "Skill",
	"icon":       "Learn",
	"schema": map[string]interface{}{
		"properties": map[string]interface{}{
			"description": map[string]interface{}{
				"title": "Description",
				"type":  "string",
			},
			"instructions": map[string]interface{}{
				"title":  "Instructions",
				"type":   "string",
				"format": "markdown",
			},
			"references": map[string]interface{}{
				"title": "References",
				"type":  "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":        map[string]interface{}{"type": "string"},
						"content":     map[string]interface{}{"type": "string"},
						"description": map[string]interface{}{"type": "string"},
					},
					"required":             []string{"path", "content"},
					"additionalProperties": false,
				},
			},
			"assets": map[string]interface{}{
				"title": "Assets",
				"type":  "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":        map[string]interface{}{"type": "string"},
						"content":     map[string]interface{}{"type": "string"},
						"description": map[string]interface{}{"type": "string"},
					},
					"required":             []string{"path", "content"},
					"additionalProperties": false,
				},
			},
			"location": map[string]interface{}{
				"title":   "Location",
				"type":    "string",
				"default": LocationGlobal,
				"enum":    []string{LocationGlobal, LocationProject},
			},
		},
		"required": []string{"description", "instructions", "location"},
	},
	"relations": map[string]interface{}{},
}

// BootstrapBlueprint ensures the skill blueprint exists in the Port catalog.
//
// Decision D4: idempotent. If the blueprint already exists (GET 200), no POST
// is issued. If GET returns 404, POST /blueprints is called. A 409 from POST
// (race condition / blueprint already created concurrently) is also treated as
// success and returns nil.
func BootstrapBlueprint(ctx context.Context, client *api.Client) error {
	// Check if the blueprint already exists.
	_, err := client.GetBlueprint(ctx, BlueprintID)
	if err == nil {
		// Blueprint exists — nothing to do.
		return nil
	}

	// Only proceed to create if the error indicates not-found.
	if !is404(err) {
		return err
	}

	// Blueprint not found — create it.
	_, createErr := client.CreateBlueprint(ctx, skillBlueprint)
	if createErr == nil {
		return nil
	}

	// 409 Conflict: another caller already created it — idempotent success.
	if is409(createErr) {
		return nil
	}

	return createErr
}

// is409 returns true when the error message indicates an HTTP 409 response from
// the Port API. It matches the status segment "failed: 409 " to avoid
// false-positives from body text that happens to contain the digit sequence "409".
func is409(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "failed: 409 ")
}
