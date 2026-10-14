package okf

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	defaultOKFThreshold      = 0.7        // for large shared bundles (100+ docs)
	defaultLocalOKFThreshold = 0.000001   // for small local bundles (< 20 docs)
)

// trustTierRank returns a numeric priority for ranking (higher = better).
// human-reviewed > machine-confirmed > unverified
func trustTierRank(tier string) int {
	switch tier {
	case "human-reviewed":
		return 3
	case "machine-confirmed":
		return 2
	default:
		return 1 // unverified or unknown
	}
}

// isStale reports whether a concept has passed its stale_after date.
// An empty staleAfter means the concept never expires.
func isStale(staleAfter string) bool {
	if staleAfter == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", staleAfter)
	if err != nil {
		// Also try full RFC3339
		t, err = time.Parse(time.RFC3339, staleAfter)
		if err != nil {
			return false // unparseable date → treat as not stale
		}
	}
	return time.Now().UTC().After(t)
}

// searchResult holds one FTS5 match row, enriched for ranking.
type searchResult struct {
	conceptID   string
	title       string
	description string
	conceptType string
	status      string
	trustTier   string
	sourceFiles string // newline-separated as stored
	staleAfter  string
	verifiedBy  string
	bm25Score   float64 // negated (positive = better)
	staleFlag   bool
	tierRank    int
}

// Query runs OKF-first search for a project.
// Returns (formatted result, bm25Score, true) when the best concept's BM25 score >= threshold.
// Returns ("", 0, false) on miss or when idx is nil.
// localOnly=true applies a much lower default threshold suited for small private bundles.
func (idx *Index) Query(_ context.Context, projectID, query string, threshold float64, localOnly bool) (string, float64, bool) {
	if idx == nil {
		return "", 0, false
	}

	bundles, ok := idx.bundles[projectID]
	if !ok || len(bundles) == 0 {
		return "", 0, false
	}

	effectiveThreshold := threshold
	if effectiveThreshold <= 0 {
		if localOnly {
			effectiveThreshold = defaultLocalOKFThreshold
		} else {
			effectiveThreshold = defaultOKFThreshold
		}
	}

	var best *searchResult

	for _, bdb := range bundles {
		results, err := queryBundle(bdb.db, query)
		if err != nil {
			// Non-fatal: skip this bundle, try next
			continue
		}

		for i := range results {
			r := &results[i]

			// Skip deprecated
			if r.status == "deprecated" {
				continue
			}
			// Only stable concepts are surfaced
			if r.status != "stable" {
				continue
			}

			r.staleFlag = isStale(r.staleAfter)
			r.tierRank = trustTierRank(r.trustTier)

			// Staleness split (Decision 6):
			// Implementation types past stale_after → filter out entirely (fall to RAG)
			if r.staleFlag && IsImplementationType(r.conceptType) {
				continue
			}

			// Compare against current best using tier-first, then bm25
			if best == nil || isBetter(r, best) {
				// Make a copy so we can safely reference it after the loop
				cp := *r
				best = &cp
			}
		}
	}

	if best == nil {
		return "", 0, false
	}

	// Threshold gate: FTS5 bm25() returns negative; we negate it so higher = better.
	// A score below the threshold means the match isn't confident enough.
	if best.bm25Score < effectiveThreshold {
		return "", 0, false
	}

	return formatOKFResult(*best), best.bm25Score, true
}

// isBetter returns true if candidate ranks higher than current.
// Ranking rule: trust tier first, then BM25 score.
func isBetter(candidate, current *searchResult) bool {
	if candidate.tierRank != current.tierRank {
		return candidate.tierRank > current.tierRank
	}
	return candidate.bm25Score > current.bm25Score
}

// queryBundle runs FTS5 MATCH against one bundle's database.
// The query is sanitized to wrap each term in quotes for FTS5 safety.
func queryBundle(db *sql.DB, query string) ([]searchResult, error) {
	// Sanitize: wrap individual terms in double quotes to prevent FTS5 syntax errors
	sanitized := sanitizeFTS5Query(query)
	if sanitized == "" {
		return nil, nil
	}

	// bm25(concepts) returns a negative float; more negative = better match.
	// We negate it (multiply by -1) so higher = better for threshold comparison.
	const sqlq = `
SELECT
    concept_id,
    title,
    description,
    concept_type,
    status,
    trust_tier,
    source_files,
    stale_after,
    verified_by,
    (-1.0 * bm25(concepts)) AS score
FROM concepts
WHERE concepts MATCH ?
  AND status != 'deprecated'
ORDER BY score DESC
LIMIT 10`

	rows, err := db.Query(sqlq, sanitized)
	if err != nil {
		return nil, fmt.Errorf("fts5 query: %w", err)
	}
	defer rows.Close()

	var results []searchResult
	for rows.Next() {
		var r searchResult
		if err := rows.Scan(
			&r.conceptID,
			&r.title,
			&r.description,
			&r.conceptType,
			&r.status,
			&r.trustTier,
			&r.sourceFiles,
			&r.staleAfter,
			&r.verifiedBy,
			&r.bm25Score,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// sanitizeFTS5Query wraps each whitespace-separated term in double-quotes
// to prevent FTS5 from interpreting special characters as operators.
func sanitizeFTS5Query(query string) string {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		// Escape any embedded double-quotes in the term
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " ")
}

// formatOKFResult renders a matched concept into the canonical MCP response format.
//
// Format:
//
//	## [OKF] {title} ({concept_type})
//	{description}
//	[⚠️ stale — content may be outdated]   ← intent types only when stale
//	Source: {source_files}
//	Trust: {trust_tier}
//	---
//	{body}   ← omitted when empty (Body is not stored in FTS5; the result has no body)
func formatOKFResult(r searchResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## [OKF] %s (%s)\n", r.title, r.conceptType))

	if r.description != "" {
		sb.WriteString(r.description)
		sb.WriteString("\n")
	}

	// Advisory stale warning for intent types (implementation types are already filtered out)
	if r.staleFlag {
		sb.WriteString("[⚠️ stale — content may be outdated]\n")
	}

	// Source files: stored as newline-separated, render as comma-separated
	if r.sourceFiles != "" {
		files := strings.Split(r.sourceFiles, "\n")
		sb.WriteString(fmt.Sprintf("Source: %s\n", strings.Join(files, ", ")))
	}

	sb.WriteString(fmt.Sprintf("Trust: %s\n", r.trustTier))

	return sb.String()
}
