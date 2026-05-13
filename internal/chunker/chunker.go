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

// MaxChunkChars is the maximum content length per chunk.
// Chunks exceeding this are split into sub-chunks of ~maxSubChunkLines lines.
// 6000 chars ≈ 1500-2400 tokens — safely under OpenAI's 8192 token limit.
const MaxChunkChars = 6000
const maxSubChunkLines = 80

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
			continue
		}

		// If the chunk is small enough, emit as-is
		if len(content) <= MaxChunkChars {
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
			continue
		}

		// Split large symbols into sub-chunks
		symbolLines := lines[symbol.StartLine-1 : symbol.EndLine]
		subChunks := splitIntoSubChunks(symbolLines, symbol, file, symbol.StartLine)
		chunks = append(chunks, subChunks...)
	}

	return chunks, nil
}

// splitIntoSubChunks divides a large symbol into smaller pieces.
// Each sub-chunk gets a suffix like "Login_part1", "Login_part2".
// The first sub-chunk includes the signature/header for context.
func splitIntoSubChunks(symbolLines []string, symbol parser.ParsedSymbol, file scanner.ScannedFile, baseStartLine int) []Chunk {
	chunks := make([]Chunk, 0)
	totalLines := len(symbolLines)
	partNum := 1

	for i := 0; i < totalLines; i += maxSubChunkLines {
		end := i + maxSubChunkLines
		if end > totalLines {
			end = totalLines
		}

		subLines := symbolLines[i:end]
		content := strings.TrimSpace(strings.Join(subLines, "\n"))
		if content == "" {
			continue
		}

		// For parts after the first, prepend a context header
		name := symbol.Name
		if partNum > 1 {
			name = fmt.Sprintf("%s_part%d", symbol.Name, partNum)
			// Add a short context line so the embedding knows what symbol this belongs to
			header := fmt.Sprintf("// continuation of %s %s", symbol.Kind, symbol.Name)
			content = header + "\n" + content
		}

		// Final safety: if still too large, hard truncate
		if len(content) > MaxChunkChars {
			content = content[:MaxChunkChars]
		}

		startLine := baseStartLine + i
		endLine := baseStartLine + end - 1

		chunks = append(chunks, Chunk{
			ChunkHash:  hashNormalized(content),
			SymbolName: name,
			SymbolType: symbol.Kind,
			StartLine:  startLine,
			EndLine:    endLine,
			Content:    content,
			TokenCount: approximateTokens(content),
			FilePath:   file.Path,
			Language:   file.Language,
		})
		partNum++
	}

	return chunks
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
