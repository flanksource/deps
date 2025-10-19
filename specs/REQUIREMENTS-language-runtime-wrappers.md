# Feature: Language Runtime Wrapper Functions

## Overview

Create wrapper functions (`RunPython`, `RunNode`, `RunJava`, `RunPowershell`) that automatically detect, install, and execute scripts in various language runtimes. These functions handle path detection, version validation, automatic installation of missing runtimes via the deps system, and wrap execution in `clicky.Exec` for robust timeout, output capture, and error handling.

**Problem Being Solved**: Users need to run scripts in various languages without manually managing runtime installations, version checking, or complex execution logic. The system should "just work" - detecting installed runtimes, installing missing ones, and providing consistent execution behavior across languages.

**Target Users**:
- Developers writing Go applications that need to execute Python/Node/Java/PowerShell scripts
- CI/CD pipelines requiring automatic runtime provisioning
- Tools that need polyglot script execution capabilities

## Functional Requirements

### FR-1: Runtime Path and Version Detection

**Description**: Each language wrapper must detect installed runtime binaries in the system PATH and verify version compatibility against specified constraints.

**User Story**: As a developer, I want the wrapper to find my installed Python/Node/Java/PowerShell runtime automatically, so that I don't have to specify paths manually.

**Acceptance Criteria**:
- [ ] Searches system PATH for runtime binaries (python/python3, node, java, powershell/pwsh)
- [ ] Executes `--version` or equivalent command to extract version string
- [ ] Parses version string into semver-compatible format
- [ ] Compares detected version against RunOptions.Version constraint (if specified)
- [ ] Caches discovered paths and versions permanently until invalidated or version mismatch
- [ ] Handles platform-specific binary names (python vs python3, pwsh vs powershell.exe)
- [ ] Returns detailed error if runtime not found or version mismatch

### FR-2: Automatic Runtime Installation via Deps

**Description**: When a required runtime is not installed or doesn't meet version constraints, automatically install it using the existing deps installation mechanism.

**User Story**: As a developer, I want missing language runtimes to be installed automatically, so that my scripts run without manual setup.

**Acceptance Criteria**:
- [ ] Determines required version from RunOptions.Version parameter
- [ ] Falls back to latest stable version if RunOptions.Version is empty/nil
- [ ] Invokes deps install logic to download and install the required runtime
- [ ] Verifies installation succeeded by re-checking version
- [ ] Updates cache with newly installed runtime path and version
- [ ] Returns detailed error with installation logs if installation fails (fail fast)
- [ ] Does not attempt retry or fallback installation methods

### FR-3: Dependency Installation with Smart Detection

**Description**: Automatically detect and install script dependencies (pip packages, npm modules) when missing, using smart detection to avoid unnecessary installations.

**User Story**: As a developer, I want script dependencies to be installed automatically when needed, so that scripts run without manual dependency management.

**Acceptance Criteria**:
- [ ] Python: Checks for requirements.txt, pyproject.toml, or setup.py in script directory
- [ ] Node: Checks for package.json in script directory
- [ ] Java: Checks for pom.xml, build.gradle, or dependencies in script directory
- [ ] Only installs dependencies if dependency files exist or imports fail
- [ ] Installs to isolated environments when possible (venv for Python, local node_modules for Node)
- [ ] Returns detailed error if dependency installation fails (fail fast)
- [ ] Caches dependency installation state to avoid redundant installations

### FR-4: Package Manager Support

**Description**: Support execution via package managers (npx, pipx, pex) for running packages without explicit installation.

**User Story**: As a developer, I want to run packages via npx/pipx/pex directly, so that I can execute tools without permanent installation.

**Acceptance Criteria**:
- [ ] Detects when script path starts with npx:, pipx:, or pex: prefix
- [ ] Extracts package name and arguments from prefixed path
- [ ] Executes via appropriate package manager (npx for Node, pipx for Python, etc.)
- [ ] Passes through all RunOptions (timeout, env vars, working dir) to package manager
- [ ] Captures and streams output consistently with direct execution
- [ ] Returns detailed error if package manager not available or execution fails

### FR-5: clicky.Exec Integration for Robust Execution

**Description**: Wrap all script execution in `clicky.Exec` to provide timeout management, output streaming/capture, working directory control, and environment variable injection.

**User Story**: As a developer, I want consistent execution behavior with timeouts and output capture, so that I can control and monitor script execution reliably.

**Acceptance Criteria**:
- [ ] All script executions use clicky.Exec as the underlying executor
- [ ] Exposes RunOptions.Timeout for execution timeout (passed to clicky.Exec)
- [ ] Exposes RunOptions.WorkingDir for setting script working directory
- [ ] Exposes RunOptions.Env for custom environment variables (merged with system env)
- [ ] Streams script output to stdout in real-time during execution
- [ ] Captures all output (stdout + stderr) and returns in result structure
- [ ] Returns execution error with full context (exit code, stdout, stderr) on failure
- [ ] Handles script interruption and cleanup on timeout

### FR-6: Platform-Specific Path and Permission Handling

**Description**: Handle platform-specific differences for Windows and Unix systems, including path separators, file extensions, and execute permissions.

**User Story**: As a developer, I want the wrapper to work correctly on both Windows and Unix systems, so that I can write cross-platform applications.

**Acceptance Criteria**:
- [ ] Uses filepath.Join for all path operations (handles Windows \ and Unix / separators)
- [ ] Appends .exe extension for Windows binaries (python.exe, node.exe, pwsh.exe)
- [ ] Checks and sets execute permissions on Unix systems before running scripts
- [ ] Handles Windows-specific PowerShell invocation (powershell.exe vs pwsh.exe)
- [ ] Detects architecture (x86, x64, arm64) and installs correct runtime binary
- [ ] Tests work correctly on both Windows and Unix (Linux/macOS) in CI

### FR-7: Language-Specific Function API

**Description**: Provide separate, ergonomic wrapper functions for each language (RunPython, RunNode, RunJava, RunPowershell) with language-specific default behaviors.

**User Story**: As a developer, I want to call RunPython() or RunNode() with clear, language-specific APIs, so that the interface is intuitive and type-safe.

**Acceptance Criteria**:
- [ ] `RunPython(script string, opts RunOptions) (RunResult, error)` function
- [ ] `RunNode(script string, opts RunOptions) (RunResult, error)` function
- [ ] `RunJava(script string, opts RunOptions) (RunResult, error)` function
- [ ] `RunPowershell(script string, opts RunOptions) (RunResult, error)` function
- [ ] RunOptions struct includes: Version, Timeout, WorkingDir, Env, InstallDeps (optional fields)
- [ ] RunResult struct includes: Stdout, Stderr, ExitCode, Duration, RuntimePath, RuntimeVersion
- [ ] Each function has language-specific defaults (e.g., python3 vs python, java vs javac)
- [ ] Clear godoc documentation with examples for each function

## User Interactions

### API Usage Flow

1. **Developer calls language-specific function**:
   ```go
   result, err := deps.RunPython("script.py", deps.RunOptions{
       Version: ">=3.9",
       Timeout: 30 * time.Second,
       WorkingDir: "/path/to/scripts",
       Env: map[string]string{"API_KEY": "secret"},
   })
   ```

2. **System performs automatic detection and setup**:
   - Checks cache for Python runtime matching version constraint
   - If not cached or version mismatch, searches PATH for python/python3
   - If not found or wrong version, auto-installs via deps
   - Detects requirements.txt and installs dependencies if needed
   - Executes script with clicky.Exec integration

3. **Developer receives result or error**:
   ```go
   if err != nil {
       log.Fatalf("Execution failed: %v", err)
   }
   fmt.Printf("Output: %s\n", result.Stdout)
   fmt.Printf("Runtime: %s version %s\n", result.RuntimePath, result.RuntimeVersion)
   ```

### Error Scenarios

**Runtime not found and installation fails**:
```
Error: failed to install Python runtime version >=3.9
Details: deps install python@3.11 failed with exit code 1
Logs: [installation output]
```

**Dependency installation fails**:
```
Error: failed to install Python dependencies from requirements.txt
Details: pip install -r requirements.txt failed with exit code 1
Stderr: [pip error output]
```

**Script execution timeout**:
```
Error: script execution exceeded timeout of 30s
Runtime: /usr/local/bin/python3 version 3.11.5
Output captured before timeout: [stdout content]
```

## Technical Considerations

### Integration Points

- **deps system**: Calls existing deps install logic for runtime installation
- **clicky.Exec**: Uses clicky.Exec for all script execution with timeout/output control
- **System PATH**: Searches PATH environment variable for installed runtimes
- **Filesystem**: Reads dependency files (requirements.txt, package.json, etc.)
- **Package managers**: Invokes pip, npm, pipx, npx for dependency/package execution

### Data Flow

1. **Inbound**: User provides script path and RunOptions
2. **Detection**: Check cache → search PATH → parse version → validate constraint
3. **Installation**: If needed, call deps install → update cache
4. **Dependency**: If needed, detect dependency files → install dependencies
5. **Execution**: Build command → set up clicky.Exec → execute → capture output
6. **Outbound**: Return RunResult with stdout/stderr/exit code or detailed error

### Caching Strategy

- **Cache Key**: `{language}-{version-constraint}` (e.g., "python->=3.9")
- **Cache Value**: `{runtime-path, runtime-version, last-validated}`
- **Cache Location**: In-memory map or persistent file in ~/.deps/cache/runtimes
- **Invalidation**: On version mismatch or explicit cache clear command
- **Persistence**: Permanent until invalidated (survives process restarts)

### Performance Requirements

- **First run with installation**: 10-60 seconds (depends on download speed)
- **Cached runtime detection**: < 100ms (read from cache, skip PATH search)
- **Version check on cache miss**: < 500ms (execute --version once)
- **Dependency installation**: 5-30 seconds (depends on package count/size)

### Security Considerations

- **Path injection**: Validate and sanitize script paths before execution
- **Command injection**: Use clicky.Exec with argument arrays, never shell string concatenation
- **Environment isolation**: Dependency installations use isolated environments when possible
- **Version pinning**: Support exact version constraints to prevent supply chain attacks

## Success Criteria

Overall definition of done:

- [ ] All four language wrapper functions (Python, Node, Java, PowerShell) implemented
- [ ] Runtime detection works correctly on Windows and Unix platforms
- [ ] Auto-installation via deps successfully installs missing runtimes
- [ ] Smart dependency detection and installation works for Python and Node
- [ ] Package manager support (npx, pipx) works correctly
- [ ] clicky.Exec integration provides timeout, output capture, and env vars
- [ ] Caching correctly avoids redundant PATH searches and version checks
- [ ] E2E tests pass on CI for all languages and platforms
- [ ] Godoc documentation complete with usage examples
- [ ] Error messages are detailed and actionable

## Testing Requirements

### E2E Tests (Primary Focus)

**Test Scenarios**:

1. **Runtime not installed → auto-install → execute**
   - Start with clean environment (runtime not in PATH)
   - Call RunPython with version constraint
   - Verify deps install invoked correctly
   - Verify script executes successfully after installation
   - Verify cache populated with installed runtime

2. **Runtime installed, version matches → execute directly**
   - Pre-install Python 3.11 in PATH
   - Call RunPython with ">=3.9" constraint
   - Verify no installation triggered
   - Verify script executes with system runtime
   - Verify cache updated

3. **Runtime installed, version mismatch → install correct version**
   - Pre-install Python 3.8 in PATH
   - Call RunPython with ">=3.9" constraint
   - Verify deps install invoked for Python 3.11
   - Verify script executes with newly installed runtime

4. **Dependencies missing → auto-install deps → execute**
   - Script with requirements.txt (e.g., requests)
   - Call RunPython without pre-installing dependencies
   - Verify pip install invoked automatically
   - Verify script executes successfully after dependency installation

5. **Package manager execution (npx)**
   - Call RunNode with "npx:cowsay hello"
   - Verify npx invoked correctly
   - Verify output captured from npx command

6. **Timeout enforcement**
   - Script with infinite loop or sleep(100)
   - Call with RunOptions.Timeout = 2 seconds
   - Verify execution terminated after 2 seconds
   - Verify timeout error returned with partial output

7. **Custom environment variables**
   - Script that reads environment variable (e.g., API_KEY)
   - Call with RunOptions.Env = {"API_KEY": "test123"}
   - Verify script receives and uses environment variable

8. **Working directory control**
   - Script that reads file from current directory
   - Call with RunOptions.WorkingDir = "/specific/path"
   - Verify script executes in specified directory
   - Verify relative paths resolved correctly

9. **Platform-specific tests**
   - Run same tests on Windows and Linux in CI
   - Verify path separators handled correctly
   - Verify .exe extensions added on Windows
   - Verify execute permissions set on Unix

10. **Error handling tests**
    - Installation failure → verify detailed error returned
    - Dependency installation failure → verify detailed error returned
    - Script execution error → verify exit code, stdout, stderr captured
    - Invalid version constraint → verify clear error message

**Test Infrastructure**:
- Use Docker containers for isolated test environments
- Mock deps install for fast unit tests (but E2E tests use real installation)
- Test matrix: {Python, Node, Java, PowerShell} × {Windows, Linux}
- CI runs E2E tests on each PR (with caching to speed up subsequent runs)

## Implementation Checklist

### Phase 1: Core Framework (Foundation)

- [ ] Define RunOptions struct (Version, Timeout, WorkingDir, Env, InstallDeps)
- [ ] Define RunResult struct (Stdout, Stderr, ExitCode, Duration, RuntimePath, RuntimeVersion)
- [ ] Design cache interface and in-memory/file-based cache implementation
- [ ] Implement platform detection utility (runtime.GOOS, runtime.GOARCH)
- [ ] Implement path utilities (search PATH, check executable, handle .exe extension)
- [ ] Create test infrastructure (Docker test environments, test scripts)

### Phase 2: Runtime Detection & Installation

- [ ] Implement generic runtime detection: searchPath(binaryName) → (path, version, error)
- [ ] Implement version parsing: parseVersion(versionOutput) → semver
- [ ] Implement version constraint checking: checkConstraint(version, constraint) → bool
- [ ] Implement cache lookup/update: getFromCache(language, constraint) / updateCache()
- [ ] Integrate with deps install: installRuntime(language, version) → error
- [ ] Add Windows-specific handling (.exe extensions, powershell vs pwsh)
- [ ] Add Unix-specific handling (execute permissions, python vs python3)

### Phase 3: Language-Specific Functions

- [ ] Implement RunPython: detect python/python3, handle venv, integrate pip
- [ ] Implement RunNode: detect node, handle node_modules, integrate npm
- [ ] Implement RunJava: detect java/javac, handle classpath, integrate maven/gradle
- [ ] Implement RunPowershell: detect pwsh/powershell, handle Windows/Unix differences
- [ ] Add smart dependency detection for Python (requirements.txt, pyproject.toml)
- [ ] Add smart dependency detection for Node (package.json)
- [ ] Add package manager support (npx, pipx, pex prefix detection and execution)

### Phase 4: clicky.Exec Integration & Execution

- [ ] Create clicky.Exec wrapper: buildExecCommand(runtime, script, opts) → clicky.Exec
- [ ] Implement timeout handling via clicky.Exec timeout parameter
- [ ] Implement working directory via clicky.Exec working dir parameter
- [ ] Implement environment variable merging and passing to clicky.Exec
- [ ] Implement output streaming (stdout) during execution
- [ ] Implement output capture (buffer stdout + stderr) for RunResult
- [ ] Implement error handling with detailed context (exit code, logs, stderr)

### Phase 5: Testing & Validation

- [ ] Write E2E test: runtime not installed → auto-install → execute
- [ ] Write E2E test: runtime installed, version matches → execute directly
- [ ] Write E2E test: runtime installed, version mismatch → install correct version
- [ ] Write E2E test: dependencies missing → auto-install deps → execute
- [ ] Write E2E test: package manager execution (npx, pipx)
- [ ] Write E2E test: timeout enforcement
- [ ] Write E2E test: custom environment variables
- [ ] Write E2E test: working directory control
- [ ] Write E2E test: platform-specific behavior (Windows + Linux in CI)
- [ ] Write E2E test: error handling (install fail, dep fail, script fail)
- [ ] Run full E2E test suite on CI for each platform

### Phase 6: Documentation & Polish

- [ ] Write godoc for RunOptions struct with field descriptions and examples
- [ ] Write godoc for RunResult struct with field descriptions
- [ ] Write godoc for RunPython with usage examples
- [ ] Write godoc for RunNode with usage examples
- [ ] Write godoc for RunJava with usage examples
- [ ] Write godoc for RunPowershell with usage examples
- [ ] Add README section with overview and quick start examples
- [ ] Document caching behavior and cache invalidation
- [ ] Document version constraint syntax (semver ranges)
- [ ] Document error scenarios and troubleshooting
- [ ] Code review and refactoring for code quality (per CLAUDE.md rules)
- [ ] Verify `make lint` passes
- [ ] Verify `make build` passes

## Appendix: Version Constraint Syntax

Support semver-style version constraints:

- `3.11` - Exact version 3.11.x
- `>=3.9` - Minimum version 3.9.0
- `>=3.9,<4.0` - Range: 3.9.0 to 3.x.x (exclude 4.0)
- `^3.9` - Compatible with 3.9.x (caret range)
- `~3.9.1` - Patch-level compatible with 3.9.1 (tilde range)
- Empty/nil - Latest stable version

## Appendix: Supported File Extensions

**Python**: .py, .pyz, .pex, pipx: prefix
**Node**: .js, .ts, .mjs, .cjs, npx: prefix
**Java**: .java, .jar, .class
**PowerShell**: .ps1, .psm1

## Appendix: Example Usage

```go
// Example 1: Simple Python script execution
result, err := deps.RunPython("analyze.py", deps.RunOptions{})
if err != nil {
    log.Fatal(err)
}
fmt.Println(result.Stdout)

// Example 2: Node script with version constraint and timeout
result, err := deps.RunNode("server.js", deps.RunOptions{
    Version: ">=18.0",
    Timeout: 30 * time.Second,
})

// Example 3: Python script with dependencies and custom env
result, err := deps.RunPython("train_model.py", deps.RunOptions{
    Version: ">=3.10",
    WorkingDir: "/data/ml",
    Env: map[string]string{
        "MODEL_PATH": "/models/v1",
        "BATCH_SIZE": "32",
    },
})

// Example 4: Package execution via npx
result, err := deps.RunNode("npx:cowsay hello world", deps.RunOptions{})

// Example 5: Java with classpath
result, err := deps.RunJava("Main.java", deps.RunOptions{
    Version: ">=17",
    Env: map[string]string{"CLASSPATH": "./lib/*"},
})
```
