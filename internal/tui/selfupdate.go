package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// detectPlatform returns the GitHub release asset suffix for the current OS/arch.
func detectPlatform() string {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch {
	case os == "linux" && arch == "amd64":
		return "linux-amd64"
	case os == "linux" && arch == "arm64":
		return "linux-arm64"
	case os == "darwin" && arch == "amd64":
		return "darwin-amd64"
	case os == "darwin" && arch == "arm64":
		return "darwin-arm64"
	case os == "windows" && arch == "amd64":
		return "windows-amd64"
	default:
		return ""
	}
}

// selfExePath returns the absolute path of the running executable.
func selfExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

// replaceBinary replaces the executable at path with new content.
// On Windows, the running exe can't be overwritten directly, so we
// rename it first, write the new binary, then remove the old one.
func replaceBinary(path string, data []byte) error {
	if runtime.GOOS == "windows" {
		return replaceBinaryWindows(path, data)
	}
	return replaceBinaryUnix(path, data)
}

func replaceBinaryUnix(path string, data []byte) error {
	// Write to a temp file next to the original, then atomic rename
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func replaceBinaryWindows(path string, data []byte) error {
	// Windows won't let us overwrite a running .exe, but we CAN rename it
	old := path + ".old"
	os.Remove(old) // clean up any previous .old

	if err := os.Rename(path, old); err != nil {
		return fmt.Errorf("rename current binary: %w", err)
	}
	if err := os.WriteFile(path, data, 0755); err != nil {
		// Try to restore
		os.Rename(old, path)
		return fmt.Errorf("write new binary: %w", err)
	}
	// Remove old binary (may fail if still locked — that's OK, cleanup next run)
	os.Remove(old)
	return nil
}
