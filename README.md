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
curl -L https://github.com/flanksource/deps/releases/latest/download/deps-linux-amd64.tar.gz -o deps.tar.gz
tar -xf deps.tar.gz
chmod +x deps
sudo mv deps /usr/local/bin/

# macOS (Apple Silicon)
curl -L https://github.com/flanksource/deps/releases/latest/download/deps-darwin-arm64.tar.gz -o deps.tar.gz
tar -xf deps.tar.gz
chmod +x deps
sudo mv deps /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/flanksource/deps/releases/latest/download/deps-darwin-amd64.tar.gz -o deps.tar.gz
tar -xf deps.tar.gz
chmod +x deps
sudo mv deps /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri https://github.com/flanksource/deps/releases/latest/download/deps-windows-amd64.exe -OutFile deps.exe
Move-Item deps.exe C:\Windows\System32\deps.exe
```
</details>

<details>
<summary><b>Using Go</b></summary>

```bash
go install github.com/flanksource/deps/cmd/deps@latest
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

The deps GitHub Action installs tools across Linux, macOS, and Windows runners with intelligent caching and parallel downloads. On Linux and macOS it can also install `deps-start`, start local services, and wait for them to become ready.

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
    version: v1.0.39  # Pin both deps and deps-start
```

### Start Services

`services` is a YAML map keyed by service name. Each service accepts the standard `deps-start` options `version`, `runtime`, `port`, `bind`, `namespace`, `data_dir`, `volume_mode`, `update`, and `wait_timeout`. Service-specific flags go under `parameters`.

```yaml
- name: Start local services
  id: services
  uses: flanksource/deps@v1
  with:
    services: |
      postgres:
        version: "17"
        runtime: docker
        port: 15432
        volume_mode: ephemeral
      clickhouse:
        version: "26.2"
        runtime: binary
        port: 19000
        wait_timeout: 4m
      opensearch:
        version: "3.8.0"
        runtime: docker
        port: 19200
        volume_mode: ephemeral
        wait_timeout: 4m
        parameters:
          jvm-memory: 512m

- name: Use service connections
  env:
    SERVICES_STARTED: ${{ steps.services.outputs.services-started }}
  run: printf '%s\n' "$SERVICES_STARTED" | yq -p=json
```

Services start sequentially in service-name order after tool installation and remain available to later job steps. Startup logs and readiness progress are written to stderr; the action captures each service's JSON result in `services-started`. That output can contain generated credentials, so avoid printing it in shared logs. Service startup is not supported on Windows because `deps-start` has no Windows release artifact and Windows does not support its background mode.

### Action Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `tools` | List of tools to install (comma-separated or multiline) | No | `""` |
| `services` | YAML map of `deps-start` services and startup options | No | `""` |
| `version` | Version of both deps and deps-start to use (e.g., `v1.0.39` or `latest`) | No | `latest` |
| `GITHUB_TOKEN` | GitHub token for accessing the API and avoiding rate limit | No | - |

### Action Outputs

| Output | Description |
|--------|-------------|
| `tools-installed` | JSON array of installed tools with versions |
| `services-started` | JSON array of started service details and connections; `[]` when no services were requested |

### Caching

The action automatically caches:
- **deps and deps-start binaries**: Cached per OS/arch/version
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

# Select a GitHub release asset by exact name, wildcard, or !exclusion.
# A bare name is retried as a prefix when no exact asset exists. The selected filename is shown in progress, and its version/platform suffix is removed when deriving the installed binary name.
deps install owner/repo --asset cli-linux --asset '!*-debug*'

# Select the newest GitHub release whose tag or title matches.
deps install owner/repo --release-filter 'v2*'
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

### Run Scripts

Execute scripts in multiple languages with automatic runtime detection and installation:

```bash
# Run Python script
deps run script.py

# Run JavaScript/Node.js script
deps run server.js

# Run TypeScript script (requires tsx or ts-node)
deps run app.ts

# Run Java program (automatically compiles and executes)
deps run Main.java

# Run PowerShell script
deps run deploy.ps1

# With version constraint
deps run --version ">=3.9" script.py
deps run --version ">=18" server.js

# With timeout
deps run --timeout 30s script.py

# With environment variables
deps run --env "API_KEY=secret" --env "DEBUG=true" script.py

# With script arguments
deps run script.py arg1 arg2

# With custom working directory
deps run --working-dir /tmp script.js

# Install dependencies automatically
deps run --install script.py
```

**Supported Languages:**
- **Python** (.py) - Auto-installs from requirements.txt
- **JavaScript** (.js, .mjs, .cjs) - Auto-installs from package.json
- **TypeScript** (.ts, .tsx) - Requires tsx or ts-node
- **Java** (.java, .jar, .class) - Auto-compiles .java files
- **PowerShell** (.ps1) - Uses pwsh or powershell

See [examples/scripts/](examples/scripts/) for example scripts.

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

## Usage: deps-start (services)

`deps-start` (a separate binary from the `start/` submodule) launches services from
the registry — postgres, opensearch, valkey, mysql, mssql, elasticsearch, loki,
prometheus, grafana, jaeger, otel-collector, clickhouse, nats, rabbitmq, rclone,
ministack, k3s, kind, kube-apiserver, trivy, activemq, activemq-artemis — via four
runtimes, and prints a
[commons-db](https://github.com/flanksource/commons-db) connection:

- **binary** — installs the artifact with deps and supervises it (clicky `SupervisedProcess`)
- **docker** — named containers via the docker SDK (honors `DOCKER_HOST` and the docker CLI context)
- **helm** — `helm upgrade --install --wait` (helm auto-installed via deps); connections
  reference chart secrets via `secret://<name>/<key>` and in-cluster `svc://` URLs
- **command** — CLI-driven lifecycles (`kind create/delete cluster`)

Services listen on 127.0.0.1 by default; `--bind 0.0.0.0` exposes them on all
interfaces, and a specific address (e.g. `--bind 192.168.1.10`) both binds and
becomes the connection host.

The same commands are available from the main binary as `deps svc <verb>`, which
forwards to `deps-start`, installing it on demand into `~/.deps/bin`:

```bash
deps svc start postgres@17
deps svc status
deps svc logs postgres -f
deps svc stop postgres
```

### Starting a service

Starting **leaves a supervisor running in the background**. The command streams the
service's logs and what it is waiting for to stderr until the service is ready or the
start timeout is reached, then returns:

| exit code | meaning |
|---|---|
| `0` | ready |
| the supervisor's own code | it exited while starting (its log is quoted in the error) |
| `1` | the start timeout was reached — the service is **left running**, follow it with `deps-start logs <service> -f` |

`--foreground`/`-f` runs the service in this terminal instead and stops it on exit.
Windows has no background mode, so services always run in the foreground there.

Starting is not an upgrade: the installed artifact is used as-is and no version is
resolved or downloaded, so a re-start is offline and near-instant. `--update` resolves the
version constraint and installs the newest match. A version given on the command line
(`postgres@17`) is always honoured; one replayed from a previous start is not a reason to
go back to the network.

### Readiness

A service is ready only when it passes **both** stages, in order:

1. the health check from its registry spec — `stdout_match`, docker `exec`, `http`
   or a TCP wait;
2. a TCP connect to its primary published port.

The second stage is what makes `stdout_match` services honest: postgres logs
"ready to accept connections" before its socket accepts, so the log line alone
is not readiness. **Restart is gated the same way** — a process that came back
or a container that is running again is not ready until it accepts connections,
and its state stays `ready: false` until it does.

While waiting, the service's own output is streamed to **stderr** prefixed with
the service name, alongside a line naming the unmet condition
(`waiting for tcp 127.0.0.1:5432 (4s)`). **stdout** carries only the structured
service envelope, so `deps-start postgres > conn.json` stays clean.

```bash
# starts postgres, prints the connection once ready, leaves it running
deps-start postgres --port 15432

# `start` is an equivalent spelling, symmetric with stop (flags follow the service)
deps-start start postgres --port 15432

# run it in this terminal instead; Ctrl-C stops the service
deps-start postgres --port 15432 -f

# pin versions with the same name@version syntax and constraint
# semantics as deps install (resolved through the package's registry)
deps-start postgres@17
deps-start start nats@2.11

# install the newest match for the constraint rather than reusing what is installed
deps-start postgres@17 --update

# lifecycle
deps-start valkey             # docker runtime (valkey has no binary artifact)
deps-start list
deps-start info valkey
deps-start logs valkey -f
deps-start restart valkey       # in place: SupervisedProcess restart / docker restart
deps-start stop --all

# helm: connection password resolves from the chart secret at hydration time
deps-start postgres --runtime helm -n dev --port 15432
# url: svc://deps-postgres.dev:15432 (the chart Service port is overridden too)
# password: secret://deps-postgres/POSTGRES_PASSWORD

# typed service-specific and Kubernetes resource flags are shown in service help
deps-start opensearch --jvm-memory 1g
deps-start opensearch --runtime helm --cpu-request 500m --memory-limit 2Gi
deps-start nats --jetstream=false

# repeating the same command reuses the running service and credentials;
# changing an option reconciles the runtime while keeping its password
deps-start nats --runtime docker --jetstream=false
deps-start nats --runtime docker --jetstream=true

# Docker uses a host bind by default. The source defaults to
# ~/.deps/services/<service>/data and the container target comes from the registry.
deps-start postgres --runtime docker --data-dir ./postgres-data
deps-start postgres --runtime docker --volume-mode persistent
deps-start postgres --runtime docker --volume-mode ephemeral

# Helm defaults to a chart-backed persistent volume. Available modes are
# chart-specific and shown in service metadata; unsupported modes fail before upgrade.
deps-start opensearch --runtime helm --volume-mode ephemeral

# JSON is the default structured service envelope on stdout. It includes the
# connection plus runtime, image/chart, parameters, ports, volume and state paths.
# Clicky lifecycle messages and configuration diffs are written to stderr.
deps-start postgres

# explicit output formats: json (default), yaml, env (connection fields only)
deps-start postgres -o yaml
deps-start postgres -o env
```

As a library:

```go
import "github.com/flanksource/deps/start"

instance, err := start.Start(ctx, "opensearch",
    start.WithPort(19200),
    start.WithParameters(map[string]string{"jvm-memory": "1g"}),
    start.WithLogWriter(os.Stderr),                    // tee the service's output while it starts
    start.WithOnWaiting(func(r start.Readiness) {      // what the readiness wait is blocked on
        log.Printf("waiting for %s (%s)", r.Waiting, r.Elapsed)
    }),
)
// instance.Connection is a commons-db models.Connection
defer instance.Stop(ctx)
```

Service metadata (ports, typed parameters, credentials, health checks, images,
charts, and connection templates) lives in the embedded registry under
`pkg/config/services*.yaml`. Runtime support per service is implied by which of
the `binary`/`docker`/`helm` blocks its `service:` spec defines. Parameters can be
limited to specific runtimes; using a Helm-only resource flag with another
runtime fails before the service starts.

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

### Why deps?

 Choose `deps` when:
 - **Embeddable Go Library**: You're building Go applications and want to embed tool management directly in your binary.
 - **Runtime Auto-Installation**: You need to run Node.js, Python, Java, or PowerShell scripts with automatic runtime and dependency installation.
 - **Complex Transformations**: You need CEL-based post-processing for advanced package manipulation (unarchive, move, chmod, etc.).
 - **Flexible Sources**: You require packages from diverse sources like Maven, Apache, or GitLab, not just GitHub releases.
 - **Project-Local**: You prefer explicit, project-local configuration (`deps.yaml`) with reproducible lock files (`deps-lock.yaml`).
 - **GitHub Action**: You want a native GitHub Action with intelligent caching and multi-platform support.


### vs. aqua (https://aquaproj.github.io/)

**aqua strengths:**
- **Massive Registry**: 20,000+ packages in the standard registry
- **Security Verification**: Built-in slsa-verifier and cosign support for supply chain security
- **Lazy Installation**: Tools installed on first use with aqua-proxy
- **Policy as Code**: Aqua Policy for governance and security controls

Choose `aqua` when:
 - You need access to a vast registry of pre-configured packages
 - Supply chain security verification (SLSA, Cosign) is critical
 - You want policy-based governance and approval workflows
 - You prefer lazy installation with proxy execution


### vs. mise (https://mise.jdx.dev/)

 **mise strengths:**
 - **System-Wide Management**: Designed for system-level tool and runtime management
 - **Environment Variables**: Built-in environment variable management per project
 - **asdf Compatibility**: Drop-in replacement for asdf with backend plugin support
 - **Version Files**: Supports .tool-versions, .mise.toml, and language-specific version files
 - **Dev Environment**: Complete development environment management
 - **Tasks**: Built-in task runner similar to Make

Choose `mise` when:
 - You want system-wide tool version management
 - You need environment variable management per directory
 - You're migrating from asdf or need asdf plugin compatibility
 - You want a task runner integrated with your tool manager
 - You need support for .tool-versions files and per-directory environments


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

## Links

- [GitHub Repository](https://github.com/flanksource/deps)
- [Issue Tracker](https://github.com/flanksource/deps/issues)
- [Releases](https://github.com/flanksource/deps/releases)
- [Examples](./examples)
