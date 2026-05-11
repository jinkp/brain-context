package retriever

import (
	"sort"
	"strings"
)

const (
	defaultMaxResults = 8
	maxAllowedResults = 20
)

func Rerank(candidates []CandidateChunk, queryTSV string, maxResults int) []CandidateChunk {
	if len(candidates) == 0 {
		return nil
	}

	queryTerms := normalizeQueryTerms(queryTSV)
	rescored := make([]CandidateChunk, len(candidates))
	copy(rescored, candidates)

	for i := range rescored {
		if len(queryTerms) > 0 {
			rescored[i].LexicalScore = lexicalScore(rescored[i].SearchVectorTokens, queryTerms)
		}
		if len(rescored[i].Relationships) > 0 {
			rescored[i].RelationshipBoost = 1
		}
		rescored[i].FinalScore = 0.70*rescored[i].SemanticScore + 0.20*rescored[i].LexicalScore + 0.10*rescored[i].RelationshipBoost
	}

	sort.SliceStable(rescored, func(i, j int) bool {
		if rescored[i].FinalScore == rescored[j].FinalScore {
			return rescored[i].SemanticScore > rescored[j].SemanticScore
		}
		return rescored[i].FinalScore > rescored[j].FinalScore
	})

	limit := clampMaxResults(maxResults)
	if len(rescored) < limit {
		limit = len(rescored)
	}
	return rescored[:limit]
}

func clampMaxResults(maxResults int) int {
	if maxResults <= 0 {
		return defaultMaxResults
	}
	if maxResults > maxAllowedResults {
		return maxAllowedResults
	}
	return maxResults
}

func normalizeQueryTerms(queryTSV string) []string {
	queryTSV = strings.TrimSpace(strings.ToLower(queryTSV))
	if queryTSV == "" {
		return nil
	}
	replacer := strings.NewReplacer("&", " ", "|", " ", "!", " ", "(", " ", ")", " ", ":*", " ", "'", " ")
	parts := strings.Fields(replacer.Replace(queryTSV))
	terms := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		terms = append(terms, part)
	}
	return terms
}

func lexicalScore(tsvText string, queryTerms []string) float64 {
	if len(queryTerms) == 0 {
		return 0
	}
	tsvText = strings.ToLower(tsvText)
	if tsvText == "" {
		return 0
	}

	matches := 0
	for _, term := range queryTerms {
		if strings.Contains(tsvText, term) {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTerms))
}
