package embedder

import (
	"context"
	"fmt"
	"strings"
)

const (
	ProviderGemini = "gemini"
	ProviderOpenAI = "openai"
	ProviderOllama = "ollama"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}

var supportedModels = map[string]int{
	"gemini/text-embedding-004":        768,
	"gemini/gemini-embedding-001":      768,
	"gemini/gemini-embedding-2":        768,
	"openai/text-embedding-3-large":    1024,
	"openai/text-embedding-3-small":    1024,
	"ollama/bge-m3":                    1024,
	"ollama/nomic-embed-text":          768,
	"ollama/nomic-embed-text:latest":   768,
}

func New(model string, apiKey string) (Embedder, error) {
	provider, normalizedModel, dimensions, err := ResolveModel(model)
	if err != nil {
		return nil, err
	}

	switch provider {
	case ProviderGemini:
		return newGeminiEmbedder(normalizedModel, apiKey, dimensions), nil
	case ProviderOpenAI:
		return newOpenAIEmbedder(normalizedModel, apiKey, dimensions), nil
	case ProviderOllama:
		return newOllamaEmbedder(normalizedModel, dimensions), nil
	default:
		return nil, fmt.Errorf("unsupported embedder provider %q", provider)
	}
}

func ResolveModel(model string) (provider string, normalizedModel string, dimensions int, err error) {
	normalizedModel = strings.TrimSpace(model)
	if normalizedModel == "" {
		return "", "", 0, fmt.Errorf("embedder model is required")
	}
	parts := strings.SplitN(normalizedModel, "/", 2)
	if len(parts) != 2 {
		return "", "", 0, fmt.Errorf("embedder model must include provider prefix, for example gemini/text-embedding-004")
	}
	provider = strings.TrimSpace(parts[0])
	modelName := strings.TrimSpace(parts[1])
	if provider == "" || modelName == "" {
		return "", "", 0, fmt.Errorf("embedder model must include provider and model name")
	}
	normalizedModel = provider + "/" + modelName
	dimensions, ok := supportedModels[normalizedModel]
	if !ok {
		return "", "", 0, fmt.Errorf("unsupported embedder model %q", normalizedModel)
	}
	if !isAllowedDimension(dimensions) {
		return "", "", 0, fmt.Errorf("model %q resolved to unsupported dimensions %d", normalizedModel, dimensions)
	}
	return provider, normalizedModel, dimensions, nil
}

func isAllowedDimension(dim int) bool {
	switch dim {
	case 768, 1024, 3072:
		return true
	default:
		return false
	}
}
