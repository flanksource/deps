# Feature: GitHub Action for Installing Tools

## Overview

A reusable GitHub Action that enables other projects to easily install development tools using the deps tool. The action will automatically download and set up the deps binary, then use it to install user-specified tools. This action will be published for use by the broader community and tested across multiple platforms (Linux, macOS, Windows).

**Problem Being Solved**: Developers need a simple, consistent way to install development tools (kubectl, helm, yq, etc.) in their GitHub Actions workflows without manually downloading binaries or managing versions.

**Target Users**:
- Repository maintainers configuring CI/CD pipelines
- DevOps engineers setting up workflows
- Open source project contributors needing consistent tooling

## Functional Requirements

### FR-1: Action Definition and Inputs
**Description**: Create an action.yml file at the repository root that defines the GitHub Action with configurable inputs for tool installation.

**User Story**: As a workflow author, I want to specify which tools to install and what version of deps to use so that I can customize the action for my project's needs.

**Acceptance Criteria**:
- [ ] `action.yml` file exists at repository root
- [ ] Input `tools` accepts comma-separated or multiline list of tool names (required)
- [ ] Input `deps-version` accepts specific deps version (optional, defaults to 'latest')
- [ ] Action metadata includes name, description, author, and branding
- [ ] All inputs are clearly documented with descriptions and examples

### FR-2: Deps Installation
**Description**: The action must automatically download and install the deps binary before attempting to install any tools.

**User Story**: As a workflow user, I want the action to handle deps installation automatically so that I don't need to manually set up deps in my workflow.

**Acceptance Criteria**:
- [ ] Action detects current platform (Linux, macOS, Windows)
- [ ] Action downloads correct deps binary for detected platform
- [ ] Deps binary is installed to appropriate location in PATH
- [ ] Installation fails immediately if deps cannot be downloaded or installed
- [ ] Deps version matches the `deps-version` input parameter

### FR-3: Tool Installation
**Description**: Install all user-specified tools using the deps binary.

**User Story**: As a workflow user, I want to install multiple tools with a single action step so that I can quickly set up my development environment.

**Acceptance Criteria**:
- [ ] Parse `tools` input (handles both comma-separated and multiline formats)
- [ ] Execute deps install command for each specified tool
- [ ] Tools are installed to standard location accessible via PATH
- [ ] Fail immediately if any tool installation fails
- [ ] Preserve installation order as specified by user

### FR-4: Caching Strategy
**Description**: Implement caching for both the deps binary and downloaded tools to improve workflow performance.

**User Story**: As a workflow user, I want subsequent runs to be faster so that I can iterate quickly during development.

**Acceptance Criteria**:
- [ ] Cache deps binary based on platform and version
- [ ] Cache downloaded tools based on tool names and platform
- [ ] Use GitHub Actions cache API (`@actions/cache`)
- [ ] Cache key includes platform, deps version, and tool list
- [ ] Restore cache at workflow start, save at workflow end

### FR-5: Installation Report
**Description**: Generate a report showing which tools were installed and their versions.

**User Story**: As a workflow user, I want to see what versions of tools were installed so that I can verify my environment and debug issues.

**Acceptance Criteria**:
- [ ] Report lists each installed tool with its version
- [ ] Report is displayed in workflow logs
- [ ] Report format is clear and easily readable
- [ ] Version information is obtained by running each tool with appropriate version flag

### FR-6: Platform Matrix Testing
**Description**: Create a test workflow that validates the action works correctly across all supported platforms.

**User Story**: As a maintainer, I want to ensure the action works on Linux, macOS, and Windows so that users on any platform can rely on it.

**Acceptance Criteria**:
- [ ] Test workflow file at `.github/workflows/test-action.yml`
- [ ] Matrix strategy includes ubuntu-latest, macos-latest, windows-latest
- [ ] Test installs multiple common tools (yq, kubectl, helm)
- [ ] Test verifies tools are accessible in PATH
- [ ] Test runs on push to main branch
- [ ] Test runs on all pull requests

## User Interactions

### GitHub Actions Workflow Usage
Users interact with this action by adding it to their workflow YAML files:

```yaml
- uses: flanksource/deps@v1
  with:
    tools: |
      yq
      kubectl
      helm
    deps-version: latest
```

### Expected User Flow
1. User adds action to their workflow file
2. Workflow triggers (on push, PR, etc.)
3. Action downloads and installs deps binary
4. Action installs each specified tool
5. Action generates installation report
6. Subsequent workflow steps can use installed tools

### Action Outputs
The action should provide outputs for use in subsequent workflow steps:
- `tools-installed`: JSON array of installed tools with versions

## Technical Considerations

### Action Implementation
- **Language**: Composite action (shell scripts) or JavaScript/TypeScript action
- **Structure**:
  - `action.yml` - Action definition
  - `scripts/install-deps.sh` - Deps installation logic (if composite)
  - `scripts/install-tools.sh` - Tool installation logic (if composite)
  - Or `dist/index.js` - Compiled JavaScript (if JS/TS action)

### Platform Detection
- Detect OS: `runner.os` context in GitHub Actions
- Detect architecture: `uname -m` or `runner.arch`
- Map to deps binary naming convention

### Deps Binary Location
- Linux/macOS: Install to `~/.local/bin` (create if not exists)
- Windows: Install to appropriate location in PATH
- Ensure location is added to PATH for subsequent steps

### Caching
- Cache key pattern: `deps-${{ runner.os }}-${{ inputs.deps-version }}-${{ hashFiles('tools-list') }}`
- Cache path:
  - `~/.local/bin/deps` (deps binary)
  - `~/.local/opt/*` (installed tools)
  - `~/.deps/*` (deps cache directory)

### Error Handling
- Validate inputs (tools list not empty)
- Check deps download success (HTTP 200, file exists)
- Verify deps execution (version check)
- Fail immediately on first tool installation error
- Provide clear error messages with troubleshooting hints

### Integration Points
- **GitHub Actions Context**: Use `@actions/core`, `@actions/cache`
- **Deps Tool**: Execute as subprocess, parse output
- **GitHub Releases**: Download deps binary from GitHub releases

## Success Criteria

Overall definition of done:
- [ ] Action can be used by external projects via `uses: flanksource/deps@v1`
- [ ] Action successfully installs tools on Linux, macOS, and Windows
- [ ] Caching reduces workflow time on subsequent runs
- [ ] Installation report provides clear visibility into installed tools
- [ ] README includes complete usage documentation with examples
- [ ] Test workflow validates action functionality
- [ ] Action handles errors gracefully with clear messages

## Testing Requirements

### Test Workflow (`.github/workflows/test-action.yml`)
```yaml
name: Test Action
on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    steps:
      - uses: actions/checkout@v4
      - uses: ./  # Test the action from local repository
        with:
          tools: |
            yq
            kubectl
            helm
          deps-version: latest
      - name: Verify installations
        run: |
          yq --version
          kubectl version --client
          helm version
```

### Test Cases
1. **Single tool installation**: Install only one tool (e.g., yq)
2. **Multiple tools**: Install 3-5 common tools
3. **Specific deps version**: Use older deps version to test version pinning
4. **Cache hit**: Run workflow twice, verify second run uses cache
5. **Invalid tool**: Attempt to install non-existent tool, verify failure

## README Updates

### Section 1: GitHub Action Usage
Add new section after installation instructions:

```markdown
## Using as a GitHub Action

You can use deps in your GitHub Actions workflows to automatically install tools:

\`\`\`yaml
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
          deps-version: latest

      - name: Use installed tools
        run: |
          yq --version
          kubectl version --client
\`\`\`
```

### Section 2: Action Inputs and Outputs

```markdown
### Action Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `tools` | List of tools to install (comma-separated or multiline) | Yes | - |
| `deps-version` | Version of deps to use | No | `latest` |

### Action Outputs

| Output | Description |
|--------|-------------|
| `tools-installed` | JSON array of installed tools with versions |

### Examples

**Install a single tool:**
\`\`\`yaml
- uses: flanksource/deps@v1
  with:
    tools: yq
\`\`\`

**Install multiple tools:**
\`\`\`yaml
- uses: flanksource/deps@v1
  with:
    tools: yq,kubectl,helm
\`\`\`

**Use specific deps version:**
\`\`\`yaml
- uses: flanksource/deps@v1
  with:
    tools: yq
    deps-version: v1.2.3
\`\`\`
```

## Implementation Checklist

### Phase 1: Action Setup
- [ ] Create `action.yml` at repository root
- [ ] Define action metadata (name, description, branding)
- [ ] Define inputs: `tools` (required), `deps-version` (optional)
- [ ] Define outputs: `tools-installed`
- [ ] Choose implementation approach (composite vs JavaScript)

### Phase 2: Core Implementation
- [ ] Implement platform detection logic
- [ ] Implement deps binary download and installation
- [ ] Implement tool parsing (comma-separated and multiline)
- [ ] Implement tool installation loop
- [ ] Implement version reporting for each tool
- [ ] Add PATH updates for installed tools

### Phase 3: Caching
- [ ] Implement cache key generation
- [ ] Add cache restore step (deps binary)
- [ ] Add cache restore step (tools)
- [ ] Add cache save step at workflow end
- [ ] Test cache hit and miss scenarios

### Phase 4: Testing
- [ ] Create `.github/workflows/test-action.yml`
- [ ] Add platform matrix (Linux, macOS, Windows)
- [ ] Add test jobs for single tool installation
- [ ] Add test jobs for multiple tool installation
- [ ] Add workflow triggers (push to main, pull requests)
- [ ] Verify tests pass on all platforms

### Phase 5: Documentation
- [ ] Add "Using as a GitHub Action" section to README
- [ ] Document all action inputs with examples
- [ ] Document action outputs
- [ ] Add multiple usage examples (single tool, multiple tools, versions)
- [ ] Add CI badge to README (optional)

### Phase 6: Verification
- [ ] Verify action works when called from external repository
- [ ] Verify caching improves performance
- [ ] Verify error messages are clear and helpful
- [ ] Verify all test workflows pass
- [ ] Code review and cleanup
- [ ] Verify all acceptance criteria met
