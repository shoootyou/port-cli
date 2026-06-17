package catalog

import (
	"context"

	"github.com/port-experimental/port-cli/internal/api"
)

// DeleteSkill removes a skill entity from the Port catalog.
// Delete is NOT idempotent (decision D3): a 404 returns a non-nil error.
func DeleteSkill(ctx context.Context, client *api.Client, identifier string) error {
	return client.DeleteEntity(ctx, BlueprintID, identifier)
}
