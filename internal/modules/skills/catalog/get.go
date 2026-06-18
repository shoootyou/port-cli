package catalog

import (
	"context"

	"github.com/port-experimental/port-cli/internal/api"
)

// GetSkill retrieves a single skill entity from the Port catalog by identifier.
func GetSkill(ctx context.Context, client *api.Client, identifier string) (SkillEntity, error) {
	entity, err := client.GetEntity(ctx, BlueprintID, identifier)
	if err != nil {
		return SkillEntity{}, err
	}
	return parseSkillEntity(entity), nil
}
