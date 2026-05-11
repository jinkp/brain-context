package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChunkPayload struct {
	ChunkHash  string
	SymbolName string
	SymbolType string
	StartLine  int
	EndLine    int
	FilePath   string
	Language   string
	TokenCount int
	Embedding  []float32
}

type projectFileRecord struct {
	ID   uuid.UUID
	Path string
}

type insertedChunkRecord struct {
	ID        uuid.UUID
	ChunkHash string
	FilePath  string
	Embedding []float32
}

func (s *Store) UpsertChunks(ctx context.Context, projectID, tenantID uuid.UUID, chunks []ChunkPayload) (accepted, skipped int, err error) {
	if len(chunks) == 0 {
		return 0, 0, nil
	}

	err = s.withTx(ctx, func(tx pgx.Tx) error {
		txCtx := ContextWithTx(ctx, tx)
		if _, ok := TxFromContext(ctx); !ok {
			if err := s.SetTenantContext(txCtx, tx, tenantID); err != nil {
				return err
			}
		}

		existing, err := findExistingChunkHashes(txCtx, tx, projectID, chunks)
		if err != nil {
			return err
		}

		newChunks := make([]ChunkPayload, 0, len(chunks))
		for _, chunk := range chunks {
			if existing[chunk.ChunkHash] {
				skipped++
				continue
			}
			newChunks = append(newChunks, chunk)
		}
		if len(newChunks) == 0 {
			return nil
		}

		filesByPath, err := upsertProjectFiles(txCtx, tx, projectID, tenantID, newChunks)
		if err != nil {
			return err
		}

		insertedChunks, err := insertChunks(txCtx, tx, projectID, tenantID, filesByPath, newChunks)
		if err != nil {
			return err
		}
		accepted = len(insertedChunks)
		skipped += len(newChunks) - accepted

		if accepted == 0 {
			return nil
		}
		if err := insertChunkVectors(txCtx, tx, projectID, tenantID, insertedChunks); err != nil {
			return err
		}
		if err := touchProjectFiles(txCtx, tx, filesByPath); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	return accepted, skipped, nil
}

func findExistingChunkHashes(ctx context.Context, exec DBTX, projectID uuid.UUID, chunks []ChunkPayload) (map[string]bool, error) {
	hashes := make([]string, 0, len(chunks))
	seen := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		hash := strings.TrimSpace(chunk.ChunkHash)
		if hash == "" || seen[hash] {
			continue
		}
		seen[hash] = true
		hashes = append(hashes, hash)
	}

	rows, err := exec.Query(ctx, `
		select chunk_hash
		from chunks
		where project_id = $1 and chunk_hash = any($2)
	`, projectID, hashes)
	if err != nil {
		return nil, fmt.Errorf("query existing chunk hashes: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("scan existing chunk hash: %w", err)
		}
		existing[hash] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing chunk hashes: %w", err)
	}
	return existing, nil
}

func upsertProjectFiles(ctx context.Context, exec DBTX, projectID, tenantID uuid.UUID, chunks []ChunkPayload) (map[string]projectFileRecord, error) {
	type fileSpec struct {
		Path     string
		Language string
	}

	byPath := make(map[string]fileSpec)
	for _, chunk := range chunks {
		path := strings.TrimSpace(chunk.FilePath)
		if path == "" {
			return nil, fmt.Errorf("chunk file_path is required")
		}
		if _, exists := byPath[path]; !exists {
			byPath[path] = fileSpec{Path: path, Language: strings.TrimSpace(chunk.Language)}
		}
	}

	ordered := make([]fileSpec, 0, len(byPath))
	for _, spec := range byPath {
		ordered = append(ordered, spec)
	}

	query, err := buildBulkInsertSQL(
		"project_files",
		[]string{"id", "tenant_id", "project_id", "path", "language", "content_hash", "size_bytes", "indexed_at"},
		len(ordered),
		"on conflict (project_id, path) do update set language = excluded.language, indexed_at = excluded.indexed_at returning id, path",
	)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	args := make([]any, 0, len(ordered)*8)
	for _, spec := range ordered {
		args = append(args,
			uuid.New(),
			tenantID,
			projectID,
			spec.Path,
			defaultString(spec.Language, "Unknown"),
			hashFilePlaceholder(spec.Path),
			int64(0),
			now,
		)
	}

	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("upsert project files: %w", err)
	}
	defer rows.Close()

	filesByPath := make(map[string]projectFileRecord, len(ordered))
	for rows.Next() {
		var record projectFileRecord
		if err := rows.Scan(&record.ID, &record.Path); err != nil {
			return nil, fmt.Errorf("scan project file row: %w", err)
		}
		filesByPath[record.Path] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project files: %w", err)
	}
	return filesByPath, nil
}

func insertChunks(ctx context.Context, exec DBTX, projectID, tenantID uuid.UUID, filesByPath map[string]projectFileRecord, chunks []ChunkPayload) ([]insertedChunkRecord, error) {
	query, err := buildBulkInsertSQL(
		"chunks",
		[]string{"id", "tenant_id", "project_id", "file_id", "chunk_hash", "symbol_name", "symbol_type", "start_line", "end_line", "content", "token_count"},
		len(chunks),
		"on conflict do nothing returning id, chunk_hash, file_id",
	)
	if err != nil {
		return nil, err
	}

	args := make([]any, 0, len(chunks)*11)
	chunkIDs := make(map[uuid.UUID]insertedChunkRecord, len(chunks))
	fileByID := make(map[uuid.UUID]string, len(filesByPath))
	for path, file := range filesByPath {
		fileByID[file.ID] = path
	}
	for _, chunk := range chunks {
		file, ok := filesByPath[chunk.FilePath]
		if !ok {
			return nil, fmt.Errorf("missing project file row for %s", chunk.FilePath)
		}
		chunkID := uuid.New()
		args = append(args,
			chunkID,
			tenantID,
			projectID,
			file.ID,
			chunk.ChunkHash,
			nullIfEmpty(chunk.SymbolName),
			nullIfEmpty(chunk.SymbolType),
			chunk.StartLine,
			chunk.EndLine,
			"",
			chunk.TokenCount,
		)
		chunkIDs[chunkID] = insertedChunkRecord{ID: chunkID, ChunkHash: chunk.ChunkHash, FilePath: chunk.FilePath, Embedding: chunk.Embedding}
	}

	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("insert chunks: %w", err)
	}
	defer rows.Close()

	inserted := make([]insertedChunkRecord, 0, len(chunks))
	for rows.Next() {
		var chunkID uuid.UUID
		var chunkHash string
		var fileID uuid.UUID
		if err := rows.Scan(&chunkID, &chunkHash, &fileID); err != nil {
			return nil, fmt.Errorf("scan inserted chunk row: %w", err)
		}
		record := chunkIDs[chunkID]
		record.ChunkHash = chunkHash
		record.FilePath = fileByID[fileID]
		inserted = append(inserted, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inserted chunks: %w", err)
	}
	return inserted, nil
}

func insertChunkVectors(ctx context.Context, exec DBTX, projectID, tenantID uuid.UUID, chunks []insertedChunkRecord) error {
	if len(chunks) == 0 {
		return nil
	}
	dimensions := len(chunks[0].Embedding)
	tableName, err := vectorTableName(dimensions)
	if err != nil {
		return err
	}

	query, err := buildBulkInsertSQL(
		tableName,
		[]string{"chunk_id", "tenant_id", "project_id", "embedding"},
		len(chunks),
		"on conflict do nothing",
	)
	if err != nil {
		return err
	}

	args := make([]any, 0, len(chunks)*4)
	for _, chunk := range chunks {
		if len(chunk.Embedding) != dimensions {
			return fmt.Errorf("mixed embedding dimensions are not supported in one upload")
		}
		args = append(args, chunk.ID, tenantID, projectID, formatVector(chunk.Embedding))
	}

	if _, err := exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert chunk vectors: %w", err)
	}
	return nil
}

func touchProjectFiles(ctx context.Context, exec DBTX, filesByPath map[string]projectFileRecord) error {
	ids := make([]uuid.UUID, 0, len(filesByPath))
	for _, file := range filesByPath {
		ids = append(ids, file.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := exec.Exec(ctx, `
		update project_files
		set indexed_at = now()
		where id = any($1)
	`, ids); err != nil {
		return fmt.Errorf("touch project files: %w", err)
	}
	return nil
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

func hashFilePlaceholder(path string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(path)))
	return hex.EncodeToString(sum[:])
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func nullIfEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
