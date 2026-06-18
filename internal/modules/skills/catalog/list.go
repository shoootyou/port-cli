package catalog

import (
	"context"

	"github.com/port-experimental/port-cli/internal/api"
)

// ListSkills retrieves all skill entities from the Port catalog.
func ListSkills(ctx context.Context, client *api.Client) ([]SkillEntity, error) {
	entities, err := client.GetEntities(ctx, BlueprintID, nil)
	if err != nil {
		return nil, err
	}

	skills := make([]SkillEntity, 0, len(entities))
	for _, e := range entities {
		skills = append(skills, parseSkillEntity(e))
	}
	return skills, nil
}
