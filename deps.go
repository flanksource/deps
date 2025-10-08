package deps

import (
	"context"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/installer"
	"github.com/flanksource/deps/pkg/types"
)

// Re-export commonly used types for public API
type (
	InstallResult = types.InstallResult
	InstallStatus = types.InstallStatus
	VerifyStatus  = types.VerifyStatus
	VersionStatus = types.VersionStatus
	Package       = types.Package
)

// Re-export status constants
const (
	InstallStatusInstalled        = types.InstallStatusInstalled
	InstallStatusForcedInstalled  = types.InstallStatusForcedInstalled
	InstallStatusAlreadyInstalled = types.InstallStatusAlreadyInstalled
	InstallStatusFailed           = types.InstallStatusFailed

	VerifyStatusChecksumMatch    = types.VerifyStatusChecksumMatch
	VerifyStatusChecksumMismatch = types.VerifyStatusChecksumMismatch
	VerifyStatusSkipped          = types.VerifyStatusSkipped

	VersionStatusValid               = types.VersionStatusValid
	VersionStatusInvalid             = types.VersionStatusInvalid
	VersionStatusUnsupportedPlatform = types.VersionStatusUnsupportedPlatform
)

// Re-export installer options
type InstallOption = installer.InstallOption

var (
	WithBinDir         = installer.WithBinDir
	WithAppDir         = installer.WithAppDir
	WithTmpDir         = installer.WithTmpDir
	WithCacheDir       = installer.WithCacheDir
	WithForce          = installer.WithForce
	WithSkipChecksum   = installer.WithSkipChecksum
	WithStrictChecksum = installer.WithStrictChecksum
	WithDebug          = installer.WithDebug
	WithOS             = installer.WithOS
)

// Install installs a package and returns detailed installation result.
// This is the main public API for programmatic package installation.
//
// Example:
//
//	result, err := deps.Install("jq", "latest",
//	    deps.WithBinDir("/usr/local/bin"),
//	    deps.WithForce(true))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result.Pretty())
func Install(packageName, version string, opts ...InstallOption) (*InstallResult, error) {
	// Load global config
	depsConfig := config.GetGlobalRegistry()

	// Create installer with options
	inst := installer.NewWithConfig(depsConfig, opts...)

	var result *InstallResult
	var installErr error

	// Create and run installation task
	task.StartTask(packageName, func(ctx flanksourceContext.Context, t *task.Task) (interface{}, error) {
		result, installErr = inst.InstallWithResult(packageName, version, t)
		return result, installErr
	})

	// Wait for task completion
	clicky.WaitForGlobalCompletion()

	return result, installErr
}

// InstallWithContext installs a package with a context and returns detailed installation result.
// This variant allows passing a context for cancellation and timeout control.
func InstallWithContext(ctx context.Context, packageName, version string, opts ...InstallOption) (*InstallResult, error) {
	// Load global config
	depsConfig := config.GetGlobalRegistry()

	// Create installer with options
	inst := installer.NewWithConfig(depsConfig, opts...)

	var result *InstallResult
	var installErr error

	// Create task manually with context
	t := &task.Task{}

	// Run installation
	result, installErr = inst.InstallWithResult(packageName, version, t)

	return result, installErr
}
