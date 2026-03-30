package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootBindsPropertiesFlag(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("properties")
	if flag == nil {
		t.Fatalf("expected properties flag to be bound")
	}
	if flag.Shorthand != "P" {
		t.Fatalf("expected properties shorthand to be P, got %q", flag.Shorthand)
	}
}

func TestSystemFlagOverridesDirs(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("system")
	if flag == nil {
		t.Fatalf("expected --system flag to be registered")
	}

	oldBin, oldApp := binDir, appDir
	t.Cleanup(func() {
		binDir = oldBin
		appDir = oldApp
		systemInstall = false
	})

	systemInstall = true
	rootCmd.PersistentPreRun(rootCmd, nil)

	if binDir != "/usr/local/bin" {
		t.Fatalf("expected binDir /usr/local/bin, got %q", binDir)
	}
	if appDir != "/usr/local" {
		t.Fatalf("expected appDir /usr/local, got %q", appDir)
	}
}

func TestRootUsageSeparatesClickyFlags(t *testing.T) {
	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	if err := rootCmd.Help(); err != nil {
		t.Fatalf("failed to render help output: %v", err)
	}

	usage := output.String()

	if !strings.Contains(usage, "\nFlags:\n") {
		t.Fatalf("expected deps flags section in usage output:\n%s", usage)
	}
	if !strings.Contains(usage, "\nClicky Flags:\n") {
		t.Fatalf("expected clicky flags section in usage output:\n%s", usage)
	}

	parts := strings.SplitN(usage, "\nClicky Flags:\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected usage output to split into deps and clicky sections:\n%s", usage)
	}

	if !strings.Contains(parts[0], "--bin-dir") {
		t.Fatalf("expected deps flags section to contain --bin-dir:\n%s", parts[0])
	}
	if strings.Contains(parts[0], "--log-level") {
		t.Fatalf("did not expect clicky flag --log-level in deps flags section:\n%s", parts[0])
	}
	if !strings.Contains(parts[1], "--log-level") {
		t.Fatalf("expected clicky flags section to contain --log-level:\n%s", parts[1])
	}
	if strings.Contains(parts[1], "--bin-dir") {
		t.Fatalf("did not expect deps flag --bin-dir in clicky flags section:\n%s", parts[1])
	}
}
