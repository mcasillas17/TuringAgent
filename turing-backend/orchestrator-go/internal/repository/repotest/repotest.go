// Package repotest holds repository fixtures shared by tests in other
// packages. Nothing outside a _test.go file may import it.
package repotest

import (
	"context"
	"fmt"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// DeleteSession drives the production withdrawal pipeline to completion:
// BeginSessionDeletion, PurgeSessionVaultArtifacts, AdvanceSessionDeletion.
// It is the single shared copy outside the repository package; that package's
// own tests keep an identical unexported one, since importing this would cycle.
func DeleteSession(ctx context.Context, repo *repository.Repository, sessionID string) error {
	if _, err := repo.BeginSessionDeletion(ctx, sessionID); err != nil {
		return err
	}
	if _, err := repo.PurgeSessionVaultArtifacts(ctx, sessionID); err != nil {
		return err
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx, sessionID, nil)
	if err != nil {
		return err
	}
	if receipt.State != "completed" {
		return fmt.Errorf("session deletion stopped at %q (%s)", receipt.State, receipt.ErrorCode)
	}
	return nil
}
