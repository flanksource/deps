package main

import (
	"fmt"
	"log"

	"github.com/flanksource/deps"
)

func main() {
	// Example 1: Install a package with minimal options
	result, err := deps.Install("jq", "latest",
		deps.WithBinDir("./bin"))
	if err != nil {
		log.Fatalf("Installation failed: %v", err)
	}

	fmt.Println(result.Pretty())
	fmt.Printf("\nInstalled %s@%s in %v\n",
		result.Package.Name,
		result.Version.Version,
		result.Duration)

	// Example 2: Install with more options
	result2, err := deps.Install("yq", "v4.35.1",
		deps.WithBinDir("./bin"),
		deps.WithForce(true),
		deps.WithSkipChecksum(true))
	if err != nil {
		log.Printf("Warning: %v", err)
	} else {
		fmt.Println(result2.Pretty())
	}

	// Example 3: Check installation status
	if result.Status == deps.InstallStatusInstalled ||
		result.Status == deps.InstallStatusForcedInstalled {
		fmt.Println("✓ Installation completed successfully")
	}
}
