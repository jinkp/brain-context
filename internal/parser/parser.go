package parser

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type ParsedSymbol struct {
	Name      string
	Kind      string
	StartLine int
	EndLine   int
	Signature string
}

type symbolPattern struct {
	regex     *regexp.Regexp
	kind      string
	nameGroup int
	lineEnd   bool
}

var (
	goMethodPattern = symbolPattern{regex: regexp.MustCompile(`^\s*func\s*\([^)]*\)\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(`), kind: "method", nameGroup: 1}
	goFuncPattern   = symbolPattern{regex: regexp.MustCompile(`^\s*func\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), kind: "func", nameGroup: 1}
	goTypePattern   = symbolPattern{regex: regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+(struct|interface|type|map|\[|chan|func|[A-Za-z_])`), kind: "type", nameGroup: 1}
	goIfacePattern  = symbolPattern{regex: regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+interface\b`), kind: "interface", nameGroup: 1}

	pythonDefPattern   = symbolPattern{regex: regexp.MustCompile(`^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), kind: "func", nameGroup: 1}
	pythonClassPattern = symbolPattern{regex: regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`), kind: "class", nameGroup: 1}

	tsFunctionPattern = symbolPattern{regex: regexp.MustCompile(`^\s*(?:export\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`), kind: "func", nameGroup: 1}
	tsClassPattern    = symbolPattern{regex: regexp.MustCompile(`^\s*(?:export\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`), kind: "class", nameGroup: 1}
	tsArrowPattern    = symbolPattern{regex: regexp.MustCompile(`^\s*(?:export\s+)?const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(?:async\s+)?\(?[^=]*\)?\s*=>`), kind: "func", nameGroup: 1}
	tsExportDefault   = symbolPattern{regex: regexp.MustCompile(`^\s*export\s+default\s+(?:function\s+)?([A-Za-z_$][A-Za-z0-9_$]*)?`), kind: "unknown", nameGroup: 1, lineEnd: true}

	javaClassPattern  = symbolPattern{regex: regexp.MustCompile(`^\s*(?:public|private|protected)?\s*(?:abstract\s+|final\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)\b`), kind: "class", nameGroup: 1}
	javaMethodPattern = symbolPattern{regex: regexp.MustCompile(`^\s*(?:public|private|protected)\s+(?:static\s+)?(?:final\s+)?(?:[A-Za-z_<>,\[\]?]+\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), kind: "method", nameGroup: 1}

	sqlTablePattern    = symbolPattern{regex: regexp.MustCompile(`(?i)^\s*create\s+table\s+(?:if\s+not\s+exists\s+)?([A-Za-z_][A-Za-z0-9_\.]*)`), kind: "table", nameGroup: 1, lineEnd: true}
	sqlFunctionPattern = symbolPattern{regex: regexp.MustCompile(`(?i)^\s*create\s+function\s+([A-Za-z_][A-Za-z0-9_\.]*)`), kind: "func", nameGroup: 1, lineEnd: true}

	defaultPattern = symbolPattern{regex: regexp.MustCompile(`^\s*(func|def|class|function|const|var|type)\s+([A-Za-z_$][A-Za-z0-9_$]*)?`), kind: "unknown", nameGroup: 2}
	multiSpace     = regexp.MustCompile(`\s+`)
)

func ParseFile(path string, language string, data []byte) ([]ParsedSymbol, error) {
	content := string(data)
	lines := strings.Split(content, "\n")
	patterns := patternsForLanguage(language)
	results := make([]ParsedSymbol, 0)

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		for _, pattern := range patterns {
			matches := pattern.regex.FindStringSubmatch(line)
			if len(matches) == 0 {
				continue
			}

			name := extractName(matches, pattern.nameGroup)
			if name == "" {
				name = fallbackName(trimmed)
			}

			results = append(results, ParsedSymbol{
				Name:      name,
				Kind:      pattern.kind,
				StartLine: index + 1,
				Signature: normalizeSignature(trimmed),
			})
			break
		}
	}

	assignEndLines(results, lines)
	if len(results) == 0 {
		return []ParsedSymbol{{
			Name:      filepath.Base(path),
			Kind:      "file",
			StartLine: 1,
			EndLine:   max(1, len(lines)),
			Signature: filepath.Base(path),
		}}, nil
	}

	return results, nil
}

func patternsForLanguage(language string) []symbolPattern {
	switch language {
	case "Go":
		return []symbolPattern{goIfacePattern, goMethodPattern, goFuncPattern, goTypePattern}
	case "Python":
		return []symbolPattern{pythonClassPattern, pythonDefPattern}
	case "TypeScript", "JavaScript":
		return []symbolPattern{tsClassPattern, tsFunctionPattern, tsArrowPattern, tsExportDefault}
	case "Java", "CSharp":
		return []symbolPattern{javaClassPattern, javaMethodPattern}
	case "SQL":
		return []symbolPattern{sqlTablePattern, sqlFunctionPattern}
	default:
		return []symbolPattern{defaultPattern}
	}
}

func assignEndLines(symbols []ParsedSymbol, lines []string) {
	if len(symbols) == 0 {
		return
	}
	lastLine := max(1, len(lines))
	for index := range symbols {
		endLine := lastLine
		if index < len(symbols)-1 {
			endLine = max(symbols[index].StartLine, symbols[index+1].StartLine-1)
		}
		symbols[index].EndLine = endLine
	}
}

func extractName(matches []string, group int) string {
	if group <= 0 || group >= len(matches) {
		return ""
	}
	return strings.TrimSpace(matches[group])
}

func fallbackName(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "unknown"
	}
	if len(fields) == 1 {
		return strings.Trim(fields[0], "({")
	}
	return strings.Trim(fields[1], "({")
}

func normalizeSignature(line string) string {
	return strings.TrimSpace(multiSpace.ReplaceAllString(line, " "))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ValidateSymbols(symbols []ParsedSymbol) error {
	for _, symbol := range symbols {
		if symbol.StartLine <= 0 || symbol.EndLine < symbol.StartLine {
			return fmt.Errorf("invalid symbol span for %s", symbol.Name)
		}
	}
	return nil
}
