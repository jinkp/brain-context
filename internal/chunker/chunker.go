package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/parser"
	"github.com/Gentleman-Programming/brain-context/internal/scanner"
)

type Chunk struct {
	ChunkHash  string
	SymbolName string
	SymbolType string
	StartLine  int
	EndLine    int
	Content    string
	TokenCount int
	FilePath   string
	Language   string
}

var whitespacePattern = regexp.MustCompile(`\s+`)

func Build(file scanner.ScannedFile, symbols []parser.ParsedSymbol, data []byte) ([]Chunk, error) {
	lines := strings.Split(string(data), "\n")
	chunks := make([]Chunk, 0, len(symbols))

	for _, symbol := range symbols {
		if symbol.StartLine <= 0 || symbol.EndLine < symbol.StartLine || symbol.EndLine > len(lines) {
			return nil, fmt.Errorf("invalid symbol range %s:%d-%d", file.Path, symbol.StartLine, symbol.EndLine)
		}

		content := strings.TrimSpace(strings.Join(lines[symbol.StartLine-1:symbol.EndLine], "\n"))
		if content == "" {
			continue // skip empty chunks — Gemini API rejects empty content
		}
		chunks = append(chunks, Chunk{
			ChunkHash:  hashNormalized(content),
			SymbolName: symbol.Name,
			SymbolType: symbol.Kind,
			StartLine:  symbol.StartLine,
			EndLine:    symbol.EndLine,
			Content:    content,
			TokenCount: approximateTokens(content),
			FilePath:   file.Path,
			Language:   file.Language,
		})
	}

	return chunks, nil
}

func hashNormalized(content string) string {
	normalized := whitespacePattern.ReplaceAllString(strings.TrimSpace(content), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func approximateTokens(content string) int {
	count := len(content) / 4
	if len(content) > 0 && count == 0 {
		return 1
	}
	return count
}
