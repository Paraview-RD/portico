package seed

import (
	"context"
	"fmt"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/service"
)

// Two codes, for the two things an administrator needs to see are possible:
// one that still works, bound to an organization so the console shows what
// redemption assigns, and one that has been shut off — the terminal state
// with no way back to ACTIVE (see
// docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md).
func (s *Seeder) seedInvitations(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}
	actor := auth.Principal{TenantID: t.tenant.ID, Username: "seed"}

	orgID := ""
	if org, ok := t.orgs["tech"]; ok {
		orgID = org.ID
	}

	if _, err := s.invitations.Create(ctx, actor, service.CreateInvitationInput{
		Code: "WELCOME-TECH", Quota: 10, OrganizationID: orgID,
	}); err != nil {
		return fmt.Errorf("create invitation WELCOME-TECH: %w", err)
	}
	w.summary.Invitations++

	disabled, err := s.invitations.Create(ctx, actor, service.CreateInvitationInput{
		Code: "OLD-BATCH", Quota: 50,
	})
	if err != nil {
		return fmt.Errorf("create invitation OLD-BATCH: %w", err)
	}
	if _, err := s.invitations.Disable(ctx, actor, disabled.ID); err != nil {
		return fmt.Errorf("disable invitation OLD-BATCH: %w", err)
	}
	w.summary.Invitations++

	return nil
}
