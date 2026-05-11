package retriever

import (
	"math"
	"testing"
)

func TestRerankSemanticScoreWinsWhenOtherFactorsAreEqual(t *testing.T) {
	t.Parallel()

	results := Rerank([]CandidateChunk{
		{ChunkHash: "lower", SemanticScore: 0.60},
		{ChunkHash: "higher", SemanticScore: 0.90},
	}, "", 2)

	if results[0].ChunkHash != "higher" {
		t.Fatalf("expected higher semantic score first, got %+v", results)
	}
}

func TestRerankRelationshipBoostAddsPointOneToFinalScore(t *testing.T) {
	t.Parallel()

	results := Rerank([]CandidateChunk{{
		ChunkHash:     "with-rel",
		SemanticScore: 0.50,
		LexicalScore:  0.50,
		Relationships: []Relationship{{RelType: "calls"}},
	}}, "", 1)

	if diff := math.Abs(results[0].FinalScore - 0.55); diff > 1e-9 {
		t.Fatalf("expected final score 0.55, got %.12f", results[0].FinalScore)
	}
}

func TestRerankSortsDescendingByFinalScore(t *testing.T) {
	t.Parallel()

	results := Rerank([]CandidateChunk{
		{ChunkHash: "third", SemanticScore: 0.20},
		{ChunkHash: "first", SemanticScore: 0.90},
		{ChunkHash: "second", SemanticScore: 0.60},
	}, "", 3)

	got := []string{results[0].ChunkHash, results[1].ChunkHash, results[2].ChunkHash}
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected order %v, got %v", want, got)
		}
	}
}

func TestRerankRespectsMaxResultsLimit(t *testing.T) {
	t.Parallel()

	results := Rerank([]CandidateChunk{
		{ChunkHash: "first", SemanticScore: 0.90},
		{ChunkHash: "second", SemanticScore: 0.80},
		{ChunkHash: "third", SemanticScore: 0.70},
	}, "", 2)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestRerankUsesExactWeightedFormula(t *testing.T) {
	t.Parallel()

	results := Rerank([]CandidateChunk{{
		ChunkHash:          "weighted",
		SemanticScore:      0.8,
		SearchVectorTokens: "alpha beta",
		Relationships:      []Relationship{{RelType: "calls"}},
	}}, "alpha", 1)

	got := results[0]
	expected := 0.70*0.8 + 0.20*1.0 + 0.10*1.0
	if diff := math.Abs(got.FinalScore - expected); diff > 1e-9 {
		t.Fatalf("expected %.12f, got %.12f", expected, got.FinalScore)
	}
}
