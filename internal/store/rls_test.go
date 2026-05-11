//go:build integration

package store

import "testing"

func TestTenantIsolationRequiresRealDatabase(t *testing.T) {
	t.Skip("integration test placeholder: requires PostgreSQL with RLS enabled")

	// Intended verification against a real database:
	// 1. Create tenant A and tenant B.
	// 2. Create one project per tenant and insert chunks for both tenants.
	// 3. Apply app.tenant_id context for tenant A.
	// 4. Query chunks/project_files through Store or raw SQL.
	// 5. Verify tenant A sees only its own rows and cannot read tenant B rows.
	// 6. Repeat for tenant B and verify symmetry.
}
