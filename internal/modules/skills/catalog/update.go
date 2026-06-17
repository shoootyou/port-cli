package catalog

import (
	"context"

	"github.com/port-experimental/port-cli/internal/api"
)

// UpdateSkill applies a partial update (PATCH) to an existing skill entity.
// It is equivalent to CreateSkill with CreateOptions{Patch: true}.
func UpdateSkill(ctx context.Context, client *api.Client, entity SkillEntity) error {
	return CreateSkill(ctx, client, entity, CreateOptions{Patch: true})
}
