# deps

**Cross-platform dependency manager with runtime auto-installation and embeddable Go library**

[![Build Status](https://github.com/flanksource/deps/actions/workflows/test.yml/badge.svg)](https://github.com/flanksource/deps/actions/workflows/test.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/flanksource/deps)](go.mod:1)
[![License](https://img.shields.io/github/license/flanksource/deps)](LICENSE)

A modern dependency manager that goes beyond simple binary installation. `deps` provides flexible package management, runtime auto-installation for Node.js/Python/Java/PowerShell scripts, and can be embedded directly into your Go applications.

---

## Features

- **7+ Package Sources**: GitHub (Releases/Tags/Builds), GitLab, Apache, Maven, direct URLs with smart auto-detection
- **Runtime Auto-Installation**: Auto-install and run Node.js, Python, Java, and PowerShell scripts with dependency management
- **Embeddable Go Library**: Simple `deps.Install()` and `deps.Run*()` APIs for Go programs
- **GitHub Action**: Native action with multi-platform support and intelligent caching
- **Built-in Registry**: Pre-configured defaults for 30+ popular tools (kubectl, helm, jq, yq, kind, postgres, maven, etc.)
- **CEL Post-Processing**: Complex transformations with `glob()`, `unarchive()`, `move()`, `chmod()` expressions
- **Lock Files**: Reproducible builds with `deps-lock.yaml` containing resolved versions and checksums
- **Directory Mode**: Install full applications with symlink management, not just single binaries
- **Checksum Verification**: Multiple strategies including inline, URL patterns, and CEL expressions
- **Version Constraints**: Semantic versioning, version pinning, or "latest" resolution with intelligent config merging

---

## Quickstart

### 1. Install deps

<details open>
<summary><b>Binary Download (Recommended)</b></summary>

```bash
# Linux (amd64)
curl -L https://github.com/flanksource/deps/releases/latest/download/deps_linux_amd64 -o deps
chmod +x deps
sudo mv deps /usr/local/bin/

# macOS (Apple Silicon)
curl -L https://github.com/flanksource/deps/releases/latest/download/deps_darwin_arm64 -o deps
chmod +x deps
sudo mv deps /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/flanksource/deps/releases/latest/download/deps_darwin_amd64 -o deps
chmod +x deps
sudo mv deps /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri https://github.com/flanksource/deps/releases/latest/download/deps_windows_amd64.exe -OutFile deps.exe
Move-Item deps.exe C:\Windows\System32\deps.exe
```
</details>

<details>
<summary><b>Using Go</b></summary>

```bash
go install github.com/flanksource/deps/cmd/deps@latest
```
</details>

<details>
<summary><b>From Source</b></summary>

```bash
git clone https://github.com/flanksource/deps
cd deps
make build
sudo mv bin/deps /usr/local/bin/
```
</details>

<details>
<summary><b>Using Homebrew</b></summary>

```bash
# Coming soon
brew install flanksource/tap/deps
```
</details>

### 2. Use deps

<details open>
<summary><b>CLI</b></summary>

```bash
# Install multiple tools at once
deps install kubectl helm jq

# Install with specific version
deps install yq@v4.40.5

# Generate lock file for reproducible builds
deps lock
```
</details>

<details>
<summary><b>GitHub Action</b></summary>

```yaml
- uses: flanksource/deps@v1
  with:
    tools: |
      yq
      kubectl
      helm
```
</details>

<details>
<summary><b>Go Library</b></summary>

```go
import "github.com/flanksource/deps"

// Install a tool
result, err := deps.Install("jq", "latest",
    deps.WithBinDir("./bin"))

// Run Python script with auto-install
result, err := deps.RunPython("script.py", deps.RunOptions{
    Version: ">=3.9",
})
```
</details>

---

## Usage: GitHub Action

The deps GitHub Action automatically installs tools across Linux, macOS, and Windows runners with intelligent caching and parallel downloads.

### Basic Usage

```yaml
name: CI
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install tools
        uses: flanksource/deps@v1
        with:
          tools: |
            yq
            kubectl
            helm

      - name: Use installed tools
        run: |
          yq --version
          kubectl version --client
          helm version
```

### Multi-Platform Matrix

```yaml
jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    steps:
      - uses: actions/checkout@v4

      - name: Install tools
        uses: flanksource/deps@v1
        with:
          tools: yq,kubectl,helm

      - name: Run tests
        run: yq --version
```

### With Version Pinning

```yaml
- uses: flanksource/deps@v1
  with:
    tools: |
      yq@v4.40.5
      kubectl@v1.28.0
      helm@v3.13.0
    version: v1.0.0  # Pin deps version
```

### Action Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `tools` | List of tools to install (comma-separated or multiline) | Yes | - |
| `version` | Version of deps to use (e.g., `v1.0.0` or `latest`) | No | `latest` |

### Action Outputs

| Output | Description |
|--------|-------------|
| `tools-installed` | JSON array of installed tools with versions |

### Caching

The action automatically caches:
- **deps binary**: Cached per OS/arch/version
- **Installed tools**: Cached per OS/arch/tool list

No manual cache configuration needed!

---

## Usage: CLI

### Install Tools

```bash
# Install from deps.yaml
deps install

# Install specific tools
deps install kubectl helm jq

# Install with version
deps install yq@v4.40.5

# Install with options
deps install kubectl --bin-dir=./tools --force
```

### Lock File Management

Generate a lock file for reproducible builds:

```bash
# Lock all dependencies in deps.yaml
deps lock

# Lock specific packages
deps lock kubectl helm

# Lock for specific platforms
deps lock --platforms linux-amd64,darwin-amd64,darwin-arm64
```

The lock file (`deps-lock.yaml`) contains resolved versions, URLs, and checksums:

```yaml
dependencies:
  - name: kubectl
    version: v1.28.0
    platforms:
      linux-amd64:
        url: https://dl.k8s.io/release/v1.28.0/bin/linux/amd64/kubectl
        checksum: sha256:abc123...
      darwin-arm64:
        url: https://dl.k8s.io/release/v1.28.0/bin/darwin/arm64/kubectl
        checksum: sha256:def456...
```

### Check and Update Tools

```bash
# Check versions of installed tools
deps check

# Check specific tool
deps check kubectl

# Check for updates
deps update

# Update specific dependency
deps update yq
```

### List Available Tools

```bash
# List all available tools from registry
deps list

# Show authentication status
deps whoami
```

---

## Usage: Go Library

Embed deps functionality directly in your Go applications.

### Basic Installation

```go
package main

import (
    "fmt"
    "log"

    "github.com/flanksource/deps"
)

func main() {
    // Install a tool
    result, err := deps.Install("jq", "latest",
        deps.WithBinDir("./bin"),
        deps.WithForce(true),
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Installed %s %s to %s\n",
        result.Package.Name,
        result.Version,
        result.Path,
    )
}
```

### Available Install Options

```go
deps.Install("kubectl", "v1.28.0",
    deps.WithBinDir("./bin"),           // Binary installation directory
    deps.WithAppDir("./apps"),          // Application directory (for directory mode)
    deps.WithTmpDir("./tmp"),           // Temporary directory
    deps.WithCacheDir("./cache"),       // Enable caching
    deps.WithForce(true),               // Force reinstall
    deps.WithSkipChecksum(false),       // Skip checksum verification
    deps.WithStrictChecksum(true),      // Fail on checksum errors
    deps.WithDebug(true),               // Enable debug logging
    deps.WithOS("linux", "amd64"),      // Override platform detection
)
```

### Runtime Execution

Run Node.js, Python, Java, or PowerShell scripts with automatic runtime installation:

```go
package main

import (
    "log"
    "time"

    "github.com/flanksource/deps"
)

func main() {
    // Run Python script
    pyResult, err := deps.RunPython("analyze.py", deps.RunOptions{
        Version: ">=3.9",
        Timeout: 30 * time.Second,
        Args:    []string{"--input", "data.csv"},
    })
    if err != nil {
        log.Fatal(err)
    }
    log.Println(pyResult.Stdout)

    // Run Node.js script
    nodeResult, err := deps.RunNode("server.js", deps.RunOptions{
        Version: ">=18.0",
        Env: map[string]string{
            "PORT": "3000",
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    // Run npx command
    npxResult, err := deps.RunNode("npx:cowsay hello", deps.RunOptions{})
    if err != nil {
        log.Fatal(err)
    }
    log.Println(npxResult.Stdout)

    // Run Java program
    javaResult, err := deps.RunJava("Main.jar", deps.RunOptions{
        Version: ">=17",
        Env: map[string]string{
            "CLASSPATH": "./lib/*",
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    // Run PowerShell script
    psResult, err := deps.RunPowershell("script.ps1", deps.RunOptions{
        Version: ">=7.0",
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

### Advanced Configuration

```go
package main

import (
    "github.com/flanksource/deps"
    "github.com/flanksource/deps/pkg/config"
    "github.com/flanksource/deps/pkg/installer"
)

func main() {
    // Load custom registry
    cfg, err := config.LoadConfigFromFile("custom-deps.yaml")
    if err != nil {
        panic(err)
    }

    // Create installer with custom config
    inst := installer.NewWithConfig(cfg,
        installer.WithBinDir("./tools"),
        installer.WithCacheDir("./.cache"),
    )

    // Install with custom config
    result, err := inst.InstallWithResult("custom-tool", "latest", nil)
    if err != nil {
        panic(err)
    }

    // Check installation status
    switch result.Status {
    case deps.InstallStatusInstalled:
        println("✓ Installed")
    case deps.InstallStatusAlreadyInstalled:
        println("✓ Already installed")
    case deps.InstallStatusFailed:
        println("✗ Failed")
    }

    // Check checksum verification
    if result.VerifyStatus == deps.VerifyStatusChecksumMatch {
        println("✓ Checksum verified")
    }
}
```

---

## Usage: Runtime Scripts

deps can automatically detect, install, and run scripts in various languages.

<details open>
<summary><b>Node.js</b></summary>

```bash
# Auto-installs Node.js if needed, runs script
deps run server.js

# With package.json, automatically runs npm install
deps run index.js

# Run npx commands
deps run npx:create-react-app my-app

# Specify Node version
deps run --runtime-version=">=18.0" server.js
```

Example with automatic dependency installation:

```json
// package.json
{
  "dependencies": {
    "express": "^4.18.0",
    "lodash": "^4.17.21"
  }
}
```

deps automatically runs `npm install` before executing your script.
</details>

<details>
<summary><b>Python</b></summary>

```bash
# Auto-installs Python if needed
deps run analyze.py

# With requirements.txt, automatically runs pip install
deps run main.py

# Specify Python version
deps run --runtime-version=">=3.9" script.py
```

Example with automatic dependency installation:

```
# requirements.txt
requests>=2.28.0
pandas>=1.5.0
numpy>=1.23.0
```

deps automatically runs `pip install -r requirements.txt` before executing your script.
</details>

<details>
<summary><b>Java</b></summary>

```bash
# Auto-installs JDK, compiles and runs
deps run HelloWorld.java

# Run JAR file
deps run application.jar

# Run compiled class
deps run com.example.Main

# With CLASSPATH
deps run --env=CLASSPATH=./lib/* Main.jar
```
</details>

<details>
<summary><b>PowerShell</b></summary>

```bash
# Auto-installs PowerShell Core (cross-platform)
deps run script.ps1

# Specify PowerShell version
deps run --runtime-version=">=7.0" advanced.ps1
```
</details>

---

## Adding Custom Dependencies

### Basic Package Definition

Create or edit `deps.yaml`:

```yaml
dependencies:
  - name: mytool
    version: v1.2.3

registry:
  mytool:
    source: github.com/owner/repo
    # Optional: specific asset pattern
    asset_pattern: "mytool-{{.Version}}-{{.OS}}-{{.Arch}}.tar.gz"
```

### Using Different Package Managers

#### GitHub Releases (Default)

```yaml
registry:
  kubectl:
    source: github.com/kubernetes/kubernetes
    # Auto-detects releases
```

#### GitHub Tags

```yaml
registry:
  tool:
    manager: github-tags
    source: github.com/owner/repo
    # Uses tags instead of releases (no API rate limits)
```

#### GitLab Releases

```yaml
registry:
  tool:
    manager: gitlab
    source: gitlab.com/group/project
```

#### Apache Archives

```yaml
registry:
  maven:
    manager: apache
    source: apache.org/maven
    extra:
      archive_path: "maven/maven-3"
```

#### Maven Repository

```yaml
registry:
  postgres:
    name: postgres-embedded
    manager: maven
    extra:
      group_id: io.zonky.test.postgres
      artifact_id: embedded-postgres-binaries-{{.os}}-{{.arch}}
      packaging: jar
      repository: https://repo1.maven.org/maven2
```

#### Direct URL

```yaml
registry:
  custom:
    url: "https://example.com/tool-{{.Version}}-{{.Platform}}.tar.gz"
```

### Directory Mode vs File Mode

**File Mode** (default): Extracts binary to bin directory

```yaml
registry:
  jq:
    source: github.com/jqlang/jq
    mode: file  # Single binary
```

**Directory Mode**: Extracts entire archive, creates symlinks

```yaml
registry:
  postgres:
    mode: directory  # Full application
    symlinks:
      - from: "pgsql/bin/*"
        to: "{{.Name}}"
```

### CEL Post-Processing

Use Common Expression Language for complex transformations:

```yaml
registry:
  tool:
    source: github.com/owner/repo
    post_process:
      # Unarchive a nested archive
      - unarchive(glob("*.zip")[0])
      # Move files
      - move("bin/tool", ".")
      # Set permissions
      - chmod("tool", 0755)
      # Delete unwanted files
      - delete(glob("*.txt"))
      # Change working directory
      - chdir("subdir")
```

Available CEL functions:
- `glob(pattern)` - Find files matching pattern
- `unarchive(file)` - Extract archive
- `move(from, to)` - Move files
- `delete(pattern)` - Delete files
- `chmod(file, mode)` - Change permissions
- `chdir(dir)` - Change directory

### Platform-Specific Configuration

```yaml
registry:
  tool:
    asset_pattern: "tool-{{.Version}}-{{.OS}}-{{.Arch}}.{{.Ext}}"
    templates:
      ext:
        windows: "zip"
        default: "tar.gz"

    # Platform-specific post-processing
    post_process:
      - condition: "{{.OS}} == 'windows'"
        steps:
          - unarchive("tool.zip")
      - condition: "{{.OS}} != 'windows'"
        steps:
          - unarchive("tool.tar.gz")
```

### Checksum Verification

#### Inline Checksum

```yaml
registry:
  tool:
    checksum: "sha256:abc123..."
```

#### Checksum URL Pattern

```yaml
registry:
  tool:
    checksum_url: "https://example.com/tool-{{.Version}}.sha256"
```

#### CEL-Based Checksum Extraction

For checksums in multi-file format:

```yaml
registry:
  tool:
    checksum_url: "https://example.com/checksums.txt"
    checksum_expr: |
      string(body).split('\n')
        .filter(line, line.contains('{{.Asset}}'))
        .map(line, line.split(' ')[0])[0]
```

### Version Expression

Custom version resolution:

```yaml
registry:
  tool:
    version_expr: |
      releases.filter(r, !r.prerelease && !r.draft)
        .map(r, r.tag_name)[0]
```

---

## Configuration

### deps.yaml Structure

```yaml
# Global settings
target: ./bin                # Binary installation directory (default: ./bin)
app_dir: ./apps             # Application directory for directory mode
cache_dir: ./.deps-cache    # Cache directory
mode: file                  # Default mode: file or directory

# Dependencies to install
dependencies:
  - name: kubectl
    version: v1.28.0

  - name: helm
    version: latest

  - name: yq
    source: github.com/mikefarah/yq  # Override source
    version: v4.40.5

# Custom package definitions
registry:
  custom-tool:
    source: github.com/owner/repo
    asset_pattern: "tool-{{.Version}}-{{.OS}}-{{.Arch}}.tar.gz"
    checksum_url: "https://example.com/checksums.txt"
    post_process:
      - unarchive(glob("*.tar.gz")[0])
      - move("bin/tool", ".")
```

### Merging with Built-in Defaults

User configurations intelligently merge with built-in defaults. You can:

1. **Override specific fields** while inheriting others:

```yaml
registry:
  kubectl:
    version: v1.28.0  # Override version, keep other kubectl defaults
```

2. **Completely replace a package definition**:

```yaml
registry:
  kubectl:
    source: custom.example.com/kubectl  # Replaces all defaults
    url: "https://custom.example.com/kubectl-{{.Version}}"
```

3. **Add new packages** alongside built-in ones

### Lock File

Generate `deps-lock.yaml` for reproducible builds:

```bash
deps lock
```

The lock file contains:
- Resolved versions
- Platform-specific URLs
- SHA256 checksums
- Download metadata

Commit `deps-lock.yaml` to version control for reproducible builds across environments.

### Authentication

Set environment variables for private repositories:

```bash
# GitHub
export GITHUB_TOKEN=ghp_...

# GitLab
export GITLAB_TOKEN=glpat-...
```

Check authentication status:

```bash
deps whoami
```

---

## Comparisons

### vs. aqua (https://aquaproj.github.io/)

**aqua strengths:**
- **Massive Registry**: 20,000+ packages in the standard registry
- **Security Verification**: Built-in slsa-verifier and cosign support for supply chain security
- **Lazy Installation**: Tools installed on first use with aqua-proxy
- **Policy as Code**: Aqua Policy for governance and security controls

**deps strengths:**
- **Embeddable Go Library**: Use `deps.Install()` directly in Go applications
- **Runtime Auto-Installation**: Automatic Node.js, Python, Java, PowerShell support with dependency management
- **CEL Post-Processing**: Powerful transformation pipeline for complex packages
- **Multiple Sources**: Maven, Apache, GitLab, GitHub builds beyond just GitHub releases
- **Directory Mode**: Full application installations with symlink management
- **GitHub Action**: Pre-built action with intelligent caching

**Choose aqua when:**
- You need access to a vast registry of pre-configured packages
- Supply chain security verification (SLSA, Cosign) is critical
- You want policy-based governance and approval workflows
- You prefer lazy installation with proxy execution

**Choose deps when:**
- You're embedding tool management in a Go application
- You need to run Node/Python/Java scripts with auto-install
- You require flexible package sources beyond GitHub (Maven, Apache, GitLab)
- You need CEL-based post-processing for complex transformations
- You're building CI/CD workflows with the GitHub Action
- You prefer project-local installations with explicit deps.yaml

---

### vs. mise (https://mise.jdx.dev/)

**mise strengths:**
- **System-Wide Management**: Designed for system-level tool and runtime management
- **Environment Variables**: Built-in environment variable management per project
- **asdf Compatibility**: Drop-in replacement for asdf with backend plugin support
- **Version Files**: Supports .tool-versions, .mise.toml, and language-specific version files
- **Dev Environment**: Complete development environment management
- **Tasks**: Built-in task runner similar to Make

**deps strengths:**
- **Embeddable in Go**: Use as a library in Go programs, not just a CLI
- **Project-Local by Default**: Tools installed per-project, not system-wide
- **GitHub Action Integration**: Native GitHub Actions support with caching
- **CEL Transformation Pipeline**: Advanced post-processing capabilities
- **Runtime Scripts**: Auto-install and run Node/Python/Java/PowerShell scripts
- **Lock Files**: Strong reproducibility with deps-lock.yaml

**Choose mise when:**
- You want system-wide tool version management
- You need environment variable management per directory
- You're migrating from asdf or need asdf plugin compatibility
- You want a task runner integrated with your tool manager
- You need support for .tool-versions files and per-directory environments

**Choose deps when:**
- You're building Go applications that need embedded dependency management
- You want project-specific tool installations without affecting the system
- You're using GitHub Actions and want native action support
- You need to run runtime scripts (Node/Python/Java) with auto-dependency installation
- You prefer explicit configuration with reproducible lock files
- You need advanced package transformations with CEL expressions

---

## Contributing

Contributions are welcome! Please see our [contributing guidelines](CONTRIBUTING.md) for details.

### Development Setup

```bash
# Clone the repository
git clone https://github.com/flanksource/deps
cd deps

# Install dependencies
go mod download

# Build
make build

# Run tests
make test

# Run linter
make lint
```

### Running Tests

```bash
# Run all tests
make test

# Run with coverage report
make test:report

# Run only failed tests
make test:failed

# Run end-to-end tests
make test:e2e
```

---

## License

deps is licensed under the [Apache License 2.0](LICENSE).

Copyright 2024 Flanksource

---

## Links

- [GitHub Repository](https://github.com/flanksource/deps)
- [Issue Tracker](https://github.com/flanksource/deps/issues)
- [Releases](https://github.com/flanksource/deps/releases)
- [Examples](./examples)
