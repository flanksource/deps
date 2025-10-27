# deps

A dependency manager for downloading and installing binary tools across multiple platforms and package managers.

## Features

- **Multi-platform support**: Automatically detects and downloads binaries for Linux, macOS (Intel/ARM), and Windows
- **Multiple package managers**: GitHub Releases, GitLab Releases, Apache archives, Maven repositories, and direct downloads
- **Lock file management**: Generate reproducible builds with `deps-lock.yaml`
- **Checksum verification**: Ensures binary integrity with SHA256 checksums
- **Version constraints**: Supports semantic versioning and version pinning
- **Authentication**: Built-in support for GitHub tokens and other auth mechanisms

## Installation

### Using Go

```bash
go install github.com/flanksource/deps/cmd/deps@latest
```

### From Source

```bash
git clone https://github.com/flanksource/deps
cd deps
make build
./bin/deps --help
```

### Using the binary directly

Download from the [releases page](https://github.com/flanksource/deps/releases).

## Quick Start

1. **Initialize a configuration file**:

```bash
deps init
```

This creates a `deps.yaml` file:

```yaml
dependencies:
  - name: yq
    source: github.com/mikefarah/yq
    version: v4.40.5
```

2. **Install dependencies**:

```bash
deps install
```

3. **Generate a lock file**:

```bash
deps lock
```

This creates `deps-lock.yaml` with resolved versions and checksums.

## Using as a GitHub Action

You can use deps in your GitHub Actions workflows to automatically install
development tools across Linux, macOS, and Windows runners.

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

### Action Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `tools` | List of tools to install (comma-separated or multiline) | Yes | - |
| `deps-version` | Version of deps to use (e.g., v1.0.0) | No | `latest` |

### Action Outputs

| Output | Description |
|--------|-------------|
| `tools-installed` | JSON array of installed tools with versions |

### Examples

**Install a single tool:**

```yaml
- uses: flanksource/deps@v1
  with:
    tools: yq
```

**Install multiple tools (comma-separated):**

```yaml
- uses: flanksource/deps@v1
  with:
    tools: yq,kubectl,helm
```

**Install multiple tools (multiline):**

```yaml
- uses: flanksource/deps@v1
  with:
    tools: |
      yq
      kubectl
      helm
      jq
```

**Use specific deps version:**

```yaml
- uses: flanksource/deps@v1
  with:
    tools: yq
    deps-version: v1.2.3
```

**Platform matrix testing:**

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
          tools: yq,kubectl

      - name: Test
        run: yq --version
```

### Features

- **Multi-platform**: Works on Linux, macOS, and Windows runners
- **Caching**: Automatically caches deps binary and installed tools for
  faster workflow runs
- **Installation report**: Generates a summary of installed tools with
  versions in the GitHub Actions UI
- **Fast installation**: Parallel downloads and smart caching minimize
  installation time

## Commands

### `deps install [tool[@version]...]`

Install dependencies from `deps.yaml` or specific tools.

```bash
# Install all dependencies
deps install

# Install specific tool
deps install yq

# Install with specific version
deps install yq@v4.40.5
```

### `deps lock [package...]`

Generate or update `deps-lock.yaml` with resolved versions and checksums.

```bash
# Lock all dependencies
deps lock

# Lock specific packages
deps lock yq kubectl
```

**Flags**:
- `--platforms`: Comma-separated list of platforms (e.g., `linux-amd64,darwin-arm64`)

### `deps update [dependency...]`

Check for and optionally update dependencies to newer versions.

```bash
# Check for updates
deps update

# Update specific dependency
deps update yq
```

### `deps check [tool...]`

Check versions of installed tools.

```bash
# Check all tools
deps check

# Check specific tool
deps check yq
```

### `deps list`

List all available dependencies from configuration.

### `deps version [tool...]`

Check installed versions of tools.

### `deps init`

Initialize a new `deps.yaml` configuration file.

### `deps whoami`

Show authentication status for package managers (GitHub, GitLab, etc.).

## Configuration

### deps.yaml

```yaml
# Target directory for installed binaries (default: ./bin)
target: ./bin

# Default installation mode
mode: file  # or "directory"

dependencies:
  - name: yq
    source: github.com/mikefarah/yq
    version: v4.40.5

  - name: kubectl
    source: github.com/kubernetes/kubernetes
    version: v1.28.0

  # Apache package manager
  - name: maven
    source: apache.org/maven
    version: 3.9.5

  # Direct download
  - name: custom-tool
    url: https://example.com/tool-{{.Version}}-{{.Platform}}.tar.gz
    version: 1.2.3
```

### deps-lock.yaml

Generated by `deps lock`, contains resolved versions and checksums:

```yaml
dependencies:
  - name: yq
    version: v4.40.5
    platforms:
      linux-amd64:
        url: https://github.com/mikefarah/yq/releases/download/v4.40.5/yq_linux_amd64
        checksum: sha256:abc123...
      darwin-arm64:
        url: https://github.com/mikefarah/yq/releases/download/v4.40.5/yq_darwin_arm64
        checksum: sha256:def456...
```

## Authentication

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

## Development

### Prerequisites

- Go 1.25 or later
- Task (optional, Makefile will auto-install)
- Ginkgo for testing

### Building

```bash
# Using make
make build

# Using task
task build

# Build for all platforms
make build-all
```

### Testing

```bash
# Run all tests
make test

# Run with report generation
make test:report

# Rerun failed tests
make test:failed
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run all checks
make check
```

## Make/Task Targets

| Target | Description |
|--------|-------------|
| `build` | Build the deps binary |
| `build-all` | Build for all supported platforms |
| `test` | Run all tests using Ginkgo |
| `test:report` | Run tests with JSON and JUnit report generation |
| `test:e2e-report` | Run e2e tests with report generation |
| `test:failed` | Rerun only failed tests from last test run |
| `lint` | Run golangci-lint |
| `fmt` | Format Go code |
| `vet` | Run go vet |
| `mod-tidy` | Tidy Go modules |
| `mod-download` | Download Go module dependencies |
| `clean` | Clean build artifacts |
| `install` | Install the binary to GOPATH/bin |
| `check` | Run all checks (fmt, vet, lint, test) |
| `ci` | Run all CI checks |

## Examples

See the [examples](./examples) directory for more configuration examples.

## License

See [LICENSE](./LICENSE) file.