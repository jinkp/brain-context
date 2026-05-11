package chunker

import (
	"testing"

	"github.com/Gentleman-Programming/brain-context/internal/parser"
	"github.com/Gentleman-Programming/brain-context/internal/scanner"
)

func TestBuildGoFunctionAsSingleChunkWithLineSpan(t *testing.T) {
	t.Parallel()

	file := scanner.ScannedFile{Path: "main.go", Language: "Go"}
	data := []byte("package main\n\nfunc greet() string {\n\treturn \"hi\"\n}")
	symbols, err := parser.ParseFile(file.Path, file.Language, data)
	if err != nil {
		t.Fatalf("parse symbols: %v", err)
	}

	chunks, err := Build(file, symbols, data)
	if err != nil {
		t.Fatalf("build chunks: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].SymbolType != "func" {
		t.Fatalf("expected func chunk, got %q", chunks[0].SymbolType)
	}
	if chunks[0].StartLine != 3 || chunks[0].EndLine != 5 {
		t.Fatalf("expected lines 3-5, got %d-%d", chunks[0].StartLine, chunks[0].EndLine)
	}
}

func TestBuildCreatesOneChunkPerSymbol(t *testing.T) {
	t.Parallel()

	file := scanner.ScannedFile{Path: "main.go", Language: "Go"}
	data := []byte("package main\n\nfunc one() {}\n\nfunc two() {}\n")
	symbols, err := parser.ParseFile(file.Path, file.Language, data)
	if err != nil {
		t.Fatalf("parse symbols: %v", err)
	}

	chunks, err := Build(file, symbols, data)
	if err != nil {
		t.Fatalf("build chunks: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].SymbolName != "one" || chunks[1].SymbolName != "two" {
		t.Fatalf("expected chunks for one and two, got %+v", chunks)
	}
}

func TestBuildFallsBackToFileChunkWhenNoSymbolsExist(t *testing.T) {
	t.Parallel()

	file := scanner.ScannedFile{Path: "README.md", Language: "Markdown"}
	data := []byte("just text\nmore text\n")
	symbols, err := parser.ParseFile(file.Path, file.Language, data)
	if err != nil {
		t.Fatalf("parse symbols: %v", err)
	}

	chunks, err := Build(file, symbols, data)
	if err != nil {
		t.Fatalf("build chunks: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 fallback chunk, got %d", len(chunks))
	}
	if chunks[0].SymbolType != "file" {
		t.Fatalf("expected fallback symbol type file, got %q", chunks[0].SymbolType)
	}
}

func TestBuildProducesDeterministicChunkHash(t *testing.T) {
	t.Parallel()

	file := scanner.ScannedFile{Path: "main.go", Language: "Go"}
	data := []byte("package main\n\nfunc greet() string {\n\treturn \"hi\"\n}")
	symbols, err := parser.ParseFile(file.Path, file.Language, data)
	if err != nil {
		t.Fatalf("parse symbols: %v", err)
	}

	first, err := Build(file, symbols, data)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	second, err := Build(file, symbols, data)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}

	if first[0].ChunkHash != second[0].ChunkHash {
		t.Fatalf("expected deterministic hash, got %q and %q", first[0].ChunkHash, second[0].ChunkHash)
	}
}
