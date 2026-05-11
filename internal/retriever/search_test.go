//go:build integration

package retriever

import "testing"

func TestSearchRequiresRealPgvectorDatabase(t *testing.T) {
	t.Skip("integration test placeholder: requires PostgreSQL + pgvector fixtures")

	// Intended verification against a real database:
	// 1. Create tenant/project fixtures with known chunk vectors in one dimension table.
	// 2. Insert search metadata (chunks, project_files, optional relationships).
	// 3. Run Search with a matching query embedding for that project's dimensions.
	// 4. Verify ANN candidates come from the correct dimension-specific vector table.
	// 5. Verify returned chunks are scoped to the requested tenant/project only.
	// 6. Verify hydrated relationship metadata is attached when symbols exist.
}
