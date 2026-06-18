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
		return wrapBlueprintHint(err)
	}

	// Upsert: merge=true by default; merge=false when Force=true (full replace).
	merge := !opts.Force
	_, err := client.UpsertSkillEntity(ctx, toAPIEntity(entity), true, merge)
	return wrapBlueprintHint(err)
}

// wrapBlueprintHint wraps a 404 error with an actionable hint to run
// `port skills catalog blueprint init`. For all other errors (including nil),
// it returns the error unchanged.
func wrapBlueprintHint(err error) error {
	if err == nil {
		return nil
	}
	if is404(err) {
		return fmt.Errorf("%w\nhint: the skill blueprint is not initialised — run `port skills catalog blueprint init`", err)
	}
	return err
}

// is404 returns true when the error message indicates an HTTP 404 response from
// the Port API (as emitted by api.Client.request). It matches the status
// segment "failed: 404 " to avoid false-positives from body text that happens
// to contain the digit sequence "404".
func is404(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "failed: 404 ")
}
