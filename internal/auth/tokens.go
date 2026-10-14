package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type TokenScope string

const (
	ScopeTenant  TokenScope = "tenant"
	ScopeProject TokenScope = "project"
	ScopeMCPRead TokenScope = "mcp_read"

	// DefaultTokenTTL is used when BRAIN_TOKEN_TTL is not set.
	// 1 year — long enough to not interrupt self-hosted workflows.
	DefaultTokenTTL = 365 * 24 * time.Hour

	tokenRandomByteLength = 32
	keyPrefixSecretLength = 12

	tenantTokenPrefix  = "brn_tenant"
	projectTokenPrefix = "brn_proj"
	mcpReadTokenPrefix = "brn_mcp"
)

// TokenTTL returns the effective token TTL.
// Reads BRAIN_TOKEN_TTL env var (e.g. "8760h", "43800h").
// Falls back to DefaultTokenTTL when unset or unparseable.
// A value of "0" or "0h" means no expiration (set to 100 years).
func TokenTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("BRAIN_TOKEN_TTL"))
	if raw == "" {
		return DefaultTokenTTL
	}
	// "0" or "0h" → effectively never expires
	if raw == "0" || raw == "0h" {
		return 100 * 365 * 24 * time.Hour
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return DefaultTokenTTL
	}
	return d
}

var (
	ErrInvalidScope = errors.New("invalid token scope")
	ErrInvalidToken = errors.New("invalid token")
)

type IssuedToken struct {
	Raw       string
	KeyPrefix string
	Scope     TokenScope
	ExpiresAt time.Time
	Hash      string
}

type ParsedToken struct {
	Raw       string
	KeyPrefix string
	Scope     TokenScope
}

func ValidateScope(scope TokenScope) error {
	switch scope {
	case ScopeTenant, ScopeProject, ScopeMCPRead:
		return nil
	default:
		return ErrInvalidScope
	}
}

func IssueToken(scope TokenScope, now time.Time) (IssuedToken, error) {
	if err := ValidateScope(scope); err != nil {
		return IssuedToken{}, err
	}

	randomBytes := make([]byte, tokenRandomByteLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return IssuedToken{}, fmt.Errorf("read random bytes: %w", err)
	}

	randomPart := base64.RawURLEncoding.EncodeToString(randomBytes)
	raw := fmt.Sprintf("%s_%s", scopePrefix(scope), randomPart)
	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("hash token: %w", err)
	}

	return IssuedToken{
		Raw:       raw,
		KeyPrefix: deriveKeyPrefix(raw),
		Scope:     scope,
		ExpiresAt: now.UTC().Add(TokenTTL()),
		Hash:      string(hash),
	}, nil
}

func ParseToken(raw string) (ParsedToken, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedToken{}, ErrInvalidToken
	}

	parsed := ParsedToken{Raw: raw, KeyPrefix: deriveKeyPrefix(raw)}
	switch {
	case strings.HasPrefix(raw, tenantTokenPrefix+"_"):
		parsed.Scope = ScopeTenant
	case strings.HasPrefix(raw, projectTokenPrefix+"_"):
		parsed.Scope = ScopeProject
	case strings.HasPrefix(raw, mcpReadTokenPrefix+"_"):
		parsed.Scope = ScopeMCPRead
	default:
		return ParsedToken{}, ErrInvalidToken
	}

	lastUnderscore := strings.LastIndex(raw, "_")
	if lastUnderscore == -1 || lastUnderscore == len(raw)-1 {
		return ParsedToken{}, ErrInvalidToken
	}

	return parsed, nil
}

func CompareTokenHash(hash, raw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw))
}

func scopePrefix(scope TokenScope) string {
	switch scope {
	case ScopeTenant:
		return tenantTokenPrefix
	case ScopeProject:
		return projectTokenPrefix
	case ScopeMCPRead:
		return mcpReadTokenPrefix
	default:
		return ""
	}
}

func deriveKeyPrefix(raw string) string {
	if raw == "" {
		return ""
	}
	lastUnderscore := strings.LastIndex(raw, "_")
	if lastUnderscore == -1 || lastUnderscore == len(raw)-1 {
		return raw
	}
	secret := raw[lastUnderscore+1:]
	if len(secret) > keyPrefixSecretLength {
		secret = secret[:keyPrefixSecretLength]
	}
	return raw[:lastUnderscore+1] + secret
}
