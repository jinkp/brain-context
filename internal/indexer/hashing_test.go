package indexer

import (
	"testing"

	"github.com/Gentleman-Programming/brain-context/internal/chunker"
	"github.com/Gentleman-Programming/brain-context/internal/scanner"
)

func TestDiffUnchangedFileHasNoChunkDiff(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{
		Files: map[string]FileSnapshot{
			"main.go": {
				ContentHash: "file-a",
				Chunks:      map[string]bool{"chunk-a": true},
			},
		},
	}
	files := []scanner.ScannedFile{{Path: "main.go", ContentHash: "file-a"}}
	chunksByFile := map[string][]chunker.Chunk{
		"main.go": {{ChunkHash: "chunk-a"}},
	}

	diff := Diff(snapshot, files, chunksByFile)

	if len(diff.NewFiles) != 0 || len(diff.ModifiedFiles) != 0 || len(diff.DeletedFiles) != 0 {
		t.Fatalf("expected no file diff, got %+v", diff)
	}
	if len(diff.NewChunks) != 0 || len(diff.ModifiedChunks) != 0 || len(diff.DeletedChunks) != 0 {
		t.Fatalf("expected no chunk diff, got %+v", diff)
	}
}

func TestDiffModifiedFileIncludesChangedChunks(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{
		Files: map[string]FileSnapshot{
			"main.go": {
				ContentHash: "old-file",
				Chunks:      map[string]bool{"old-chunk": true},
			},
		},
	}
	files := []scanner.ScannedFile{{Path: "main.go", ContentHash: "new-file"}}
	chunksByFile := map[string][]chunker.Chunk{
		"main.go": {{ChunkHash: "new-chunk"}},
	}

	diff := Diff(snapshot, files, chunksByFile)

	assertChunkRefsEqual(t, diff.ModifiedChunks, []ChunkRef{{FilePath: "main.go", ChunkHash: "new-chunk"}})
	assertChunkRefsEqual(t, diff.DeletedChunks, []ChunkRef{{FilePath: "main.go", ChunkHash: "old-chunk"}})
}

func TestDiffNewFileIncludesAllChunksAsNew(t *testing.T) {
	t.Parallel()

	files := []scanner.ScannedFile{{Path: "main.go", ContentHash: "file-a"}}
	chunksByFile := map[string][]chunker.Chunk{
		"main.go": {{ChunkHash: "chunk-a"}, {ChunkHash: "chunk-b"}},
	}

	diff := Diff(Snapshot{Files: map[string]FileSnapshot{}}, files, chunksByFile)

	if len(diff.NewFiles) != 1 || diff.NewFiles[0] != "main.go" {
		t.Fatalf("expected new file main.go, got %+v", diff.NewFiles)
	}
	assertChunkRefsEqual(t, diff.NewChunks, []ChunkRef{{FilePath: "main.go", ChunkHash: "chunk-a"}, {FilePath: "main.go", ChunkHash: "chunk-b"}})
}

func TestDiffDeletedFileAppearsInDeletedFiles(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{
		Files: map[string]FileSnapshot{
			"main.go": {
				ContentHash: "file-a",
				Chunks:      map[string]bool{"chunk-a": true},
			},
		},
	}

	diff := Diff(snapshot, nil, map[string][]chunker.Chunk{})

	if len(diff.DeletedFiles) != 1 || diff.DeletedFiles[0] != "main.go" {
		t.Fatalf("expected deleted file main.go, got %+v", diff.DeletedFiles)
	}
	assertChunkRefsEqual(t, diff.DeletedChunks, []ChunkRef{{FilePath: "main.go", ChunkHash: "chunk-a"}})
}

func TestDiffModifiedFileOnlyIncludesChangedChunk(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{
		Files: map[string]FileSnapshot{
			"main.go": {
				ContentHash: "old-file",
				Chunks:      map[string]bool{"chunk-a": true, "chunk-b": true},
			},
		},
	}
	files := []scanner.ScannedFile{{Path: "main.go", ContentHash: "new-file"}}
	chunksByFile := map[string][]chunker.Chunk{
		"main.go": {{ChunkHash: "chunk-a"}, {ChunkHash: "chunk-c"}},
	}

	diff := Diff(snapshot, files, chunksByFile)

	assertChunkRefsEqual(t, diff.ModifiedChunks, []ChunkRef{{FilePath: "main.go", ChunkHash: "chunk-c"}})
}

func assertChunkRefsEqual(t *testing.T, got, want []ChunkRef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d chunk refs, got %d (%+v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected chunk ref %d to be %+v, got %+v", i, want[i], got[i])
		}
	}
}
