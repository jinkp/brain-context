package main

import (
	"strings"
	"testing"
)

// ── runOKF routing (4.10 — lightweight integration) ──────────────────────────

func TestRunOKF_NoArgs_PrintsUsage(t *testing.T) {
	// runOKF with no args should print usage and return nil (not an error)
	err := runOKF([]string{})
	if err != nil {
		t.Errorf("runOKF with no args: expected nil error, got %v", err)
	}
}

func TestRunOKF_RegisterSubcommand_ReturnsError_MissingFlags(t *testing.T) {
	// runOKF with "register" but no flags → error (missing --project and --bundle)
	err := runOKF([]string{"register"})
	if err == nil {
		t.Error("runOKF register with no flags: expected error, got nil")
	}
}

func TestRunOKF_UnknownSubcommand_PrintsUsage(t *testing.T) {
	// runOKF with unknown subcommand should print usage and return nil
	err := runOKF([]string{"unknown-cmd"})
	if err != nil {
		t.Errorf("runOKF with unknown subcommand: expected nil error, got %v", err)
	}
}

func TestRunOKF_GenerateSubcommand_UnknownProject(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Set up a config with no "nonexistent" project
	writeEmptyConfig(t, tmpHome)

	err := runOKF([]string{"generate", "--project", "nonexistent"})
	if err == nil {
		t.Error("runOKF generate with unknown project: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention project name 'nonexistent': %v", err)
	}
}

func TestRunOKF_ListSubcommand_UnknownProject(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	writeEmptyConfig(t, tmpHome)

	err := runOKF([]string{"list", "--project", "ghost"})
	if err == nil {
		t.Error("runOKF list with unknown project: expected error, got nil")
	}
}

func TestRunOKF_ValidateSubcommand_UnknownProject(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	writeEmptyConfig(t, tmpHome)

	err := runOKF([]string{"validate", "--project", "ghost"})
	if err == nil {
		t.Error("runOKF validate with unknown project: expected error, got nil")
	}
}
