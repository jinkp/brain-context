package retriever

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/google/uuid"
)

const annCandidateLimit = 50

type Relationship struct {
	RelType string  `json:"rel_type"`
	DstType string  `json:"dst_type"`
	DstName string  `json:"dst_name"`
	Weight  float64 `json:"weight,omitempty"`
}

type CandidateChunk struct {
	ChunkID            uuid.UUID      `json:"chunk_id"`
	ChunkHash          string         `json:"chunk_hash"`
	SymbolName         string         `json:"symbol_name"`
	SymbolType         string         `json:"symbol_type"`
	FilePath           string         `json:"file_path"`
	StartLine          int            `json:"start_line"`
	EndLine            int            `json:"end_line"`
	Language           string         `json:"language"`
	TokenCount         int            `json:"token_count"`
	SemanticScore      float64        `json:"semantic_score"`
	LexicalScore       float64        `json:"lexical_score"`
	RelationshipBoost  float64        `json:"relationship_boost"`
	FinalScore         float64        `json:"final_score"`
	Relationships      []Relationship `json:"relationships,omitempty"`
	SearchVectorTokens string         `json:"-"`
}

func Search(ctx context.Context, db store.DBTX, projectID, tenantID uuid.UUID, queryEmbedding []float32, dimensions int, maxChunks int) ([]CandidateChunk, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("query embedding is required")
	}
	if len(queryEmbedding) != dimensions {
		return nil, fmt.Errorf("query embedding dimensions %d do not match project dimensions %d", len(queryEmbedding), dimensions)
	}

	tableName, err := vectorTableName(dimensions)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		select
			c.id,
			c.chunk_hash,
			coalesce(c.symbol_name, ''),
			coalesce(c.symbol_type, ''),
			coalesce(c.start_line, 0),
			coalesce(c.end_line, 0),
			coalesce(c.token_count, 0),
			coalesce(c.tsv::text, ''),
			pf.path,
			pf.language,
			greatest(0, least(1, 1 - (cv.embedding <=> $1::vector))) as semantic_score
		from %s cv
		join chunks c on c.id = cv.chunk_id
		join project_files pf on pf.id = c.file_id
		where cv.project_id = $2 and cv.tenant_id = $3
		order by cv.embedding <=> $1::vector
		limit %d
	`, tableName, annCandidateLimit)

	rows, err := db.Query(ctx, query, formatVector(queryEmbedding), projectID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query ANN candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]CandidateChunk, 0, minPositive(maxChunks, annCandidateLimit))
	symbolNames := make([]string, 0, annCandidateLimit)
	seenSymbols := make(map[string]struct{})
	for rows.Next() {
		var candidate CandidateChunk
		if err := rows.Scan(
			&candidate.ChunkID,
			&candidate.ChunkHash,
			&candidate.SymbolName,
			&candidate.SymbolType,
			&candidate.StartLine,
			&candidate.EndLine,
			&candidate.TokenCount,
			&candidate.SearchVectorTokens,
			&candidate.FilePath,
			&candidate.Language,
			&candidate.SemanticScore,
		); err != nil {
			return nil, fmt.Errorf("scan ANN candidate: %w", err)
		}
		if name := strings.TrimSpace(candidate.SymbolName); name != "" {
			if _, exists := seenSymbols[name]; !exists {
				seenSymbols[name] = struct{}{}
				symbolNames = append(symbolNames, name)
			}
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ANN candidates: %w", err)
	}

	if len(candidates) == 0 || len(symbolNames) == 0 {
		return candidates, nil
	}

	relationshipsBySymbol, err := loadRelationships(ctx, db, projectID, tenantID, symbolNames)
	if err != nil {
		return nil, err
	}
	for i := range candidates {
		candidates[i].Relationships = relationshipsBySymbol[candidates[i].SymbolName]
	}

	return candidates, nil
}

func loadRelationships(ctx context.Context, db store.DBTX, projectID, tenantID uuid.UUID, symbolNames []string) (map[string][]Relationship, error) {
	rows, err := db.Query(ctx, `
		select
			src.fq_name,
			r.rel_type,
			r.dst_type,
			coalesce(dst.fq_name, dst_file.path, ''),
			coalesce(r.weight, 0)
		from relationships r
		join symbols src
			on src.id = r.src_id
			and src.project_id = r.project_id
			and src.tenant_id = r.tenant_id
		left join symbols dst
			on r.dst_type = 'symbol'
			and dst.id = r.dst_id
			and dst.project_id = r.project_id
			and dst.tenant_id = r.tenant_id
		left join project_files dst_file
			on r.dst_type = 'file'
			and dst_file.id = r.dst_id
			and dst_file.project_id = r.project_id
			and dst_file.tenant_id = r.tenant_id
		where r.project_id = $1
			and r.tenant_id = $2
			and r.src_type = 'symbol'
			and src.fq_name = any($3)
	`, projectID, tenantID, symbolNames)
	if err != nil {
		return nil, fmt.Errorf("query relationships: %w", err)
	}
	defer rows.Close()

	relationshipsBySymbol := make(map[string][]Relationship, len(symbolNames))
	for rows.Next() {
		var srcName string
		var relationship Relationship
		if err := rows.Scan(&srcName, &relationship.RelType, &relationship.DstType, &relationship.DstName, &relationship.Weight); err != nil {
			return nil, fmt.Errorf("scan relationship: %w", err)
		}
		relationshipsBySymbol[srcName] = append(relationshipsBySymbol[srcName], relationship)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate relationships: %w", err)
	}

	return relationshipsBySymbol, nil
}

func vectorTableName(dimensions int) (string, error) {
	switch dimensions {
	case 768:
		return "chunk_vectors_768", nil
	case 1024:
		return "chunk_vectors_1024", nil
	case 3072:
		return "chunk_vectors_3072", nil
	default:
		return "", fmt.Errorf("unsupported embedding dimensions %d", dimensions)
	}
}

func formatVector(values []float32) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%g", value))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func minPositive(value, fallback int) int {
	if value > 0 && value < fallback {
		return value
	}
	return fallback
}
