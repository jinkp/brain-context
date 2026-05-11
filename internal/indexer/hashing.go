package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/chunker"
	"github.com/Gentleman-Programming/brain-context/internal/scanner"
)

type Snapshot struct {
	Files     map[string]FileSnapshot `json:"files"`
	UpdatedAt time.Time               `json:"updated_at"`
}

type FileSnapshot struct {
	ContentHash string          `json:"content_hash"`
	Chunks      map[string]bool `json:"chunks"`
}

type ChunkRef struct {
	FilePath  string `json:"file_path"`
	ChunkHash string `json:"chunk_hash"`
}

type DiffResult struct {
	NewFiles      []string   `json:"new_files"`
	ModifiedFiles []string   `json:"modified_files"`
	DeletedFiles  []string   `json:"deleted_files"`
	NewChunks     []ChunkRef `json:"new_chunks"`
	ModifiedChunks []ChunkRef `json:"modified_chunks"`
	DeletedChunks []ChunkRef `json:"deleted_chunks"`
}

func LoadSnapshot(projectID string) (Snapshot, error) {
	path, err := snapshotPath(projectID)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, os.ErrNotExist
		}
		return Snapshot{}, fmt.Errorf("stat snapshot: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("parse snapshot: %w", err)
	}
	if snapshot.Files == nil {
		snapshot.Files = map[string]FileSnapshot{}
	}
	return snapshot, nil
}

func SaveSnapshot(projectID string, files []scanner.ScannedFile, chunksByFile map[string][]chunker.Chunk) error {
	path, err := snapshotPath(projectID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}

	snapshot := Snapshot{
		Files:     map[string]FileSnapshot{},
		UpdatedAt: time.Now().UTC(),
	}
	for _, file := range files {
		chunkSet := map[string]bool{}
		for _, chunk := range chunksByFile[file.Path] {
			chunkSet[chunk.ChunkHash] = true
		}
		snapshot.Files[file.Path] = FileSnapshot{
			ContentHash: file.ContentHash,
			Chunks:      chunkSet,
		}
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

func Diff(snapshot Snapshot, files []scanner.ScannedFile, chunksByFile map[string][]chunker.Chunk) DiffResult {
	result := DiffResult{
		NewFiles:       []string{},
		ModifiedFiles:  []string{},
		DeletedFiles:   []string{},
		NewChunks:      []ChunkRef{},
		ModifiedChunks: []ChunkRef{},
		DeletedChunks:  []ChunkRef{},
	}

	currentFiles := make(map[string]scanner.ScannedFile, len(files))
	for _, file := range files {
		currentFiles[file.Path] = file
		previous, exists := snapshot.Files[file.Path]
		if !exists {
			result.NewFiles = append(result.NewFiles, file.Path)
			for _, chunk := range chunksByFile[file.Path] {
				result.NewChunks = append(result.NewChunks, ChunkRef{FilePath: file.Path, ChunkHash: chunk.ChunkHash})
			}
			continue
		}

		if previous.ContentHash == file.ContentHash {
			continue
		}

		result.ModifiedFiles = append(result.ModifiedFiles, file.Path)
		currentChunks := toChunkSet(chunksByFile[file.Path])
		for hash := range currentChunks {
			if !previous.Chunks[hash] {
				result.ModifiedChunks = append(result.ModifiedChunks, ChunkRef{FilePath: file.Path, ChunkHash: hash})
			}
		}
		for hash := range previous.Chunks {
			if !currentChunks[hash] {
				result.DeletedChunks = append(result.DeletedChunks, ChunkRef{FilePath: file.Path, ChunkHash: hash})
			}
		}
	}

	for path, previous := range snapshot.Files {
		if _, exists := currentFiles[path]; exists {
			continue
		}
		result.DeletedFiles = append(result.DeletedFiles, path)
		for hash := range previous.Chunks {
			result.DeletedChunks = append(result.DeletedChunks, ChunkRef{FilePath: path, ChunkHash: hash})
		}
	}

	sort.Strings(result.NewFiles)
	sort.Strings(result.ModifiedFiles)
	sort.Strings(result.DeletedFiles)
	sortChunkRefs(result.NewChunks)
	sortChunkRefs(result.ModifiedChunks)
	sortChunkRefs(result.DeletedChunks)
	return result
}

func HasSnapshot(projectID string) (bool, error) {
	path, err := snapshotPath(projectID)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat snapshot: %w", err)
}

func snapshotPath(projectID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".brain", "snapshots", projectID+".json"), nil
}

func toChunkSet(chunks []chunker.Chunk) map[string]bool {
	set := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		set[chunk.ChunkHash] = true
	}
	return set
}

func sortChunkRefs(chunks []ChunkRef) {
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].FilePath == chunks[j].FilePath {
			return chunks[i].ChunkHash < chunks[j].ChunkHash
		}
		return chunks[i].FilePath < chunks[j].FilePath
	})
}
