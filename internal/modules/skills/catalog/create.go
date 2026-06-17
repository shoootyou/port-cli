package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/port-experimental/port-cli/internal/api"
)

// CreateSkill provisions or updates a skill entity in the Port catalog.
//
// Behaviour by option combination:
//
//   - Default (Force=false, Patch=false): POST with upsert=true&merge=true
//   - Force=true, Patch=false:           POST with upsert=true&merge=false
//   - Patch=true, Force=false:           PATCH /blueprints/skill/entities/<id>
//   - Force=true AND Patch=true:         error before any HTTP call (mutually exclusive)
//
// When the skill blueprint does not exist (404), the error message contains
// "port skills catalog blueprint init" as an actionable hint.
func CreateSkill(ctx context.Context, client *api.Client, entity SkillEntity, opts CreateOptions) error {
	if opts.Force && opts.Patch {
		return fmt.Errorf("--force and --patch are mutually exclusive")
	}

	if opts.Patch {
		_, err := client.PatchSkillEntity(ctx, entity.Identifier, toAPIEntity(entity))
		return err
	}

	// Upsert: merge=true by default; merge=false when Force=true (full replace).
	merge := !opts.Force
	_, err := client.UpsertSkillEntity(ctx, toAPIEntity(entity), true, merge)
	if err != nil {
		if is404(err) {
			return fmt.Errorf("%w\nhint: the skill blueprint is not initialised — run `port skills catalog blueprint init`", err)
		}
		return err
	}
	return nil
}

// is404 returns true when the error message indicates an HTTP 404 response from
// the Port API (as emitted by api.Client.request).
func is404(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "Not Found")
}
