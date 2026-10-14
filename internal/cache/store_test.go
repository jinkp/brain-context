package cache_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/cache"
)

// openTestStore opens an isolated cache DB in t.TempDir().
func openTestStore(t *testing.T) *cache.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "context-cache.db")
	store, err := cache.OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func makeState(projectID, model string, dims int, lastIndexed string) cache.ProjectState {
	return cache.ProjectState{
		ProjectID:       projectID,
		ProjectName:     projectID + "-name",
		LastIndexed:     lastIndexed,
		EmbedModel:      model,
		EmbedDimensions: dims,
		APIEndpoint:     "http://localhost:8080",
		UpdatedAt:       time.Now().UTC(),
	}
}

// ── Open / schema ─────────────────────────────────────────────────────────────

func TestOpen_CreatesDB(t *testing.T) {
	store := openTestStore(t)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestOpen_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context-cache.db")
	for i := range 3 {
		s, err := cache.OpenAt(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		s.Close()
	}
}

// ── ProjectState ─────────────────────────────────────────────────────────────

func TestProjectState_RoundTrip(t *testing.T) {
	store := openTestStore(t)

	want := makeState("proj-abc", "text-embedding-3-small", 1536, "2026-10-01T00:00:00Z")
	if err := store.UpsertProjectState(want); err != nil {
		t.Fatalf("UpsertProjectState: %v", err)
	}

	got, err := store.GetProjectState("proj-abc")
	if err != nil {
		t.Fatalf("GetProjectState: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
	if got.ProjectID != want.ProjectID {
		t.Errorf("ProjectID: got %q, want %q", got.ProjectID, want.ProjectID)
	}
	if got.LastIndexed != want.LastIndexed {
		t.Errorf("LastIndexed: got %q, want %q", got.LastIndexed, want.LastIndexed)
	}
	if got.EmbedModel != want.EmbedModel {
		t.Errorf("EmbedModel: got %q, want %q", got.EmbedModel, want.EmbedModel)
	}
	if got.EmbedDimensions != want.EmbedDimensions {
		t.Errorf("EmbedDimensions: got %d, want %d", got.EmbedDimensions, want.EmbedDimensions)
	}
}

func TestGetProjectState_MissingReturnsNil(t *testing.T) {
	store := openTestStore(t)

	got, err := store.GetProjectState("nonexistent")
	if err != nil {
		t.Fatalf("GetProjectState: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown project, got %+v", got)
	}
}

func TestUpsertProjectState_UpdatesExisting(t *testing.T) {
	store := openTestStore(t)

	initial := makeState("proj-x", "model-a", 768, "2026-01-01T00:00:00Z")
	if err := store.UpsertProjectState(initial); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := makeState("proj-x", "model-b", 1536, "2026-06-01T00:00:00Z")
	if err := store.UpsertProjectState(updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := store.GetProjectState("proj-x")
	if err != nil || got == nil {
		t.Fatalf("GetProjectState after update: %v", err)
	}
	if got.EmbedModel != "model-b" {
		t.Errorf("expected model-b, got %q", got.EmbedModel)
	}
	if got.LastIndexed != "2026-06-01T00:00:00Z" {
		t.Errorf("expected updated LastIndexed, got %q", got.LastIndexed)
	}
}

// ── Cache hit / miss ──────────────────────────────────────────────────────────

func TestGet_HitOnExactKey(t *testing.T) {
	store := openTestStore(t)
	state := makeState("proj-1", "model-x", 512, "2026-10-01T00:00:00Z")

	key, err := cache.BuildKey("proj-1", "search_project_context", "how does auth work", nil, state)
	if err != nil {
		t.Fatalf("BuildKey: %v", err)
	}

	if err := store.Put(key, "## Relevant Context\n...", cache.DefaultTTL); err != nil {
		t.Fatalf("Put: %v", err)
	}

	text, hit, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if text != "## Relevant Context\n..." {
		t.Errorf("text mismatch: %q", text)
	}
}

func TestGet_MissOnExpiredEntry(t *testing.T) {
	store := openTestStore(t)
	state := makeState("proj-1", "model-x", 512, "2026-10-01T00:00:00Z")

	key, _ := cache.BuildKey("proj-1", "search_project_context", "some query", nil, state)

	// Store with tiny TTL that's already past.
	if err := store.Put(key, "stale response", -1*time.Second); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, hit, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Error("expected cache miss for expired entry")
	}
}

func TestGet_MissOnUnknownKey(t *testing.T) {
	store := openTestStore(t)
	state := makeState("proj-2", "model-y", 768, "2026-10-01T00:00:00Z")

	key, _ := cache.BuildKey("proj-2", "explain_flow", "login flow", nil, state)

	_, hit, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Error("expected miss for never-stored key")
	}
}

// ── Freshness invalidation ─────────────────────────────────────────────────────

func TestGet_MissWhenLastIndexedChanges(t *testing.T) {
	store := openTestStore(t)

	stateV1 := makeState("proj-fresh", "model-x", 512, "2026-10-01T00:00:00Z")
	key, _ := cache.BuildKey("proj-fresh", "search_project_context", "payment retry", nil, stateV1)

	if err := store.Put(key, "old response", cache.DefaultTTL); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Simulate a new index: last_indexed changed.
	stateV2 := makeState("proj-fresh", "model-x", 512, "2026-10-10T00:00:00Z")
	keyV2, _ := cache.BuildKey("proj-fresh", "search_project_context", "payment retry", nil, stateV2)

	_, hit, err := store.Get(keyV2)
	if err != nil {
		t.Fatalf("Get v2: %v", err)
	}
	if hit {
		t.Error("expected cache miss when last_indexed changed")
	}
}

func TestGet_MissWhenEmbedModelChanges(t *testing.T) {
	store := openTestStore(t)

	stateA := makeState("proj-embed", "model-a", 512, "2026-10-01T00:00:00Z")
	key, _ := cache.BuildKey("proj-embed", "search_project_context", "some query", nil, stateA)
	if err := store.Put(key, "response a", cache.DefaultTTL); err != nil {
		t.Fatalf("Put: %v", err)
	}

	stateB := makeState("proj-embed", "model-b", 512, "2026-10-01T00:00:00Z")
	keyB, _ := cache.BuildKey("proj-embed", "search_project_context", "some query", nil, stateB)

	_, hit, err := store.Get(keyB)
	if err != nil {
		t.Fatalf("Get stateB: %v", err)
	}
	if hit {
		t.Error("expected cache miss when embed_model changed")
	}
}

// ── Parameter-sensitive keys ──────────────────────────────────────────────────

func TestBuildKey_DifferentMaxChunks(t *testing.T) {
	state := makeState("proj-params", "model-x", 512, "2026-10-01T00:00:00Z")

	key8, err := cache.BuildKey("proj-params", "search_project_context", "auth flow",
		map[string]any{"max_chunks": 8}, state)
	if err != nil {
		t.Fatalf("BuildKey 8: %v", err)
	}

	key20, err := cache.BuildKey("proj-params", "search_project_context", "auth flow",
		map[string]any{"max_chunks": 20}, state)
	if err != nil {
		t.Fatalf("BuildKey 20: %v", err)
	}

	store := openTestStore(t)
	if err := store.Put(key8, "response for 8", cache.DefaultTTL); err != nil {
		t.Fatalf("Put key8: %v", err)
	}

	_, hit, err := store.Get(key20)
	if err != nil {
		t.Fatalf("Get key20: %v", err)
	}
	if hit {
		t.Error("different max_chunks must produce different cache entries")
	}
}

func TestBuildKey_QueryNormalization(t *testing.T) {
	// Same semantic query, different whitespace/casing → same key
	state := makeState("proj-norm", "model-x", 512, "")

	key1, _ := cache.BuildKey("proj-norm", "search_project_context", "  How Does Auth WORK  ", nil, state)
	key2, _ := cache.BuildKey("proj-norm", "search_project_context", "how does auth work", nil, state)

	store := openTestStore(t)
	if err := store.Put(key1, "normalised response", cache.DefaultTTL); err != nil {
		t.Fatalf("Put key1: %v", err)
	}

	text, hit, err := store.Get(key2)
	if err != nil {
		t.Fatalf("Get key2: %v", err)
	}
	if !hit {
		t.Error("normalised query should produce same cache key")
	}
	if text != "normalised response" {
		t.Errorf("unexpected text: %q", text)
	}
}

// ── Project-scoped isolation ──────────────────────────────────────────────────

func TestClear_OnlyAffectsTargetProject(t *testing.T) {
	store := openTestStore(t)

	stateA := makeState("proj-a", "model-x", 512, "")
	stateB := makeState("proj-b", "model-x", 512, "")

	for i := range 3 {
		k, _ := cache.BuildKey("proj-a", "search_project_context", fmt.Sprintf("query-%d", i), nil, stateA)
		_ = store.Put(k, "response a", cache.DefaultTTL)
	}
	kB, _ := cache.BuildKey("proj-b", "search_project_context", "query-b", nil, stateB)
	_ = store.Put(kB, "response b", cache.DefaultTTL)

	n, err := store.Clear("proj-a")
	if err != nil {
		t.Fatalf("Clear proj-a: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 deleted, got %d", n)
	}

	// proj-b must still be accessible.
	text, hit, err := store.Get(kB)
	if err != nil {
		t.Fatalf("Get proj-b after clear: %v", err)
	}
	if !hit {
		t.Error("proj-b entry should survive proj-a clear")
	}
	if text != "response b" {
		t.Errorf("unexpected text: %q", text)
	}
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func TestStats_CountsActiveAndExpired(t *testing.T) {
	store := openTestStore(t)
	state := makeState("proj-stats", "model-x", 512, "")

	// 2 active entries
	for i := range 2 {
		k, _ := cache.BuildKey("proj-stats", "search_project_context", fmt.Sprintf("active-%d", i), nil, state)
		_ = store.Put(k, "response", cache.DefaultTTL)
	}
	// 1 expired entry
	kExpired, _ := cache.BuildKey("proj-stats", "explain_flow", "expired", nil, state)
	_ = store.Put(kExpired, "old", -1*time.Second)

	stats, err := store.Stats("proj-stats")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalCount != 3 {
		t.Errorf("TotalCount: got %d, want 3", stats.TotalCount)
	}
	if stats.ActiveCount != 2 {
		t.Errorf("ActiveCount: got %d, want 2", stats.ActiveCount)
	}
}

// ── Prune ─────────────────────────────────────────────────────────────────────

func TestPrune_RemovesOnlyExpired(t *testing.T) {
	store := openTestStore(t)
	state := makeState("proj-prune", "model-x", 512, "")

	kActive, _ := cache.BuildKey("proj-prune", "search_project_context", "active query", nil, state)
	_ = store.Put(kActive, "fresh response", cache.DefaultTTL)

	kExpired, _ := cache.BuildKey("proj-prune", "explain_flow", "expired query", nil, state)
	_ = store.Put(kExpired, "stale response", -1*time.Second)

	n, err := store.Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 pruned, got %d", n)
	}

	// Active entry must survive.
	_, hit, err := store.Get(kActive)
	if err != nil {
		t.Fatalf("Get after prune: %v", err)
	}
	if !hit {
		t.Error("active entry should survive Prune")
	}
}
