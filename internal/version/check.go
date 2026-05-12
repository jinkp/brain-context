// Package version checks for newer brain-context releases on GitHub.
package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	repoOwner = "jinkp"
	repoName  = "brain-context"
)

var (
	checkTimeout           = 3 * time.Second
	githubLatestReleaseURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)
	httpClient             = http.DefaultClient
)

type CheckStatus string

const (
	StatusUpToDate        CheckStatus = "up_to_date"
	StatusUpdateAvailable CheckStatus = "update_available"
	StatusCheckFailed     CheckStatus = "check_failed"
)

type CheckResult struct {
	Status        CheckStatus
	Message       string
	LatestVersion string
	CurrentVersion string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

// CheckLatest compares the running version against the latest GitHub release.
func CheckLatest(current string) CheckResult {
	switch current {
	case "", "dev":
		return checkFailed("")
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return checkFailed("")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return checkFailed("")
		}
		return checkFailed("")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return checkFailed("")
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return checkFailed("")
	}

	latest := normalizeVersion(release.TagName)
	running := normalizeVersion(current)

	if latest == "" || !isNewer(latest, running) {
		return CheckResult{Status: StatusUpToDate, CurrentVersion: running, LatestVersion: latest}
	}

	return CheckResult{
		Status:         StatusUpdateAvailable,
		CurrentVersion: running,
		LatestVersion:  latest,
		Message:        updateInstructions(),
	}
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func isNewer(latest, current string) bool {
	l := splitVersion(latest)
	c := splitVersion(current)
	for i := 0; i < 3; i++ {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}
	return false
}

func splitVersion(v string) [3]int {
	var parts [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		if i >= 3 {
			break
		}
		for _, c := range s {
			if c >= '0' && c <= '9' {
				parts[i] = parts[i]*10 + int(c-'0')
			} else {
				break
			}
		}
	}
	return parts
}

func updateInstructions() string {
	switch runtime.GOOS {
	case "windows":
		return "irm https://raw.githubusercontent.com/jinkp/brain-context/master/install.ps1 | iex"
	default:
		return "curl -fsSL https://raw.githubusercontent.com/jinkp/brain-context/master/install.sh | bash"
	}
}

func githubToken() string {
	if t := strings.TrimSpace(os.Getenv("GH_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func checkFailed(msg string) CheckResult {
	return CheckResult{Status: StatusCheckFailed, Message: msg}
}
