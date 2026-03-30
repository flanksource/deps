package cmd

import "testing"

func TestRootBindsPropertiesFlag(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("properties")
	if flag == nil {
		t.Fatalf("expected properties flag to be bound")
	}
	if flag.Shorthand != "P" {
		t.Fatalf("expected properties shorthand to be P, got %q", flag.Shorthand)
	}
}
