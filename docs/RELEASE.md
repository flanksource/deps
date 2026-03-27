# Release Process

This project uses automated releases that trigger on every merge to the `main` branch.

## How it works

1. **Automatic Version Bumping**: When code is merged to `main`, the auto-release workflow analyzes commit messages using [Conventional Commits](https://www.conventionalcommits.org/) to determine the next version.

2. **Tag Creation**: If changes warrant a release, a new tag is automatically created and pushed.

3. **Release Creation**: The existing release workflow detects the new tag and uses GoReleaser to build binaries and create a GitHub release.

## Commit Message Format

Use [Conventional Commits](https://www.conventionalcommits.org/) format for automatic version bumping:

- `feat: add new feature` → Minor version bump (0.1.0 → 0.2.0)
- `fix: resolve bug` → Patch version bump (0.1.0 → 0.1.1)
- `feat!: breaking change` → Major version bump (0.1.0 → 1.0.0)
- `chore: update dependencies` → Patch version bump (0.1.0 → 0.1.1)
- `docs: update README` → Patch version bump (0.1.0 → 0.1.1)

## Manual Release

If you need to create a manual release:

```bash
# Create and push a tag
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

The release workflow will automatically trigger and create the GitHub release with binaries.

## Workflow Files

- `.github/workflows/release.yml` - Automated release workflow that triggers on main branch pushes. Creates version tags and builds binaries.
- `.github/workflows/update-base-image.yml` - Automatically creates a PR on flanksource/base-image to update the deps version after a release is published.
- `.github/workflows/test.yml` - Unit and integration tests
- `.github/workflows/test-action.yml` - Tests the GitHub Action functionality
- `.github/workflows/golangci-lint.yml` - Code quality checks

## Version Calculation

The release workflow uses [svu](https://github.com/caarlos0/svu) to calculate the next version based on:

1. Conventional commit messages since the last tag
2. Current semantic version from the latest tag
3. Automatically creates patch versions on every main branch push

## Cross-Repository Updates

After a release is published, the `update-base-image.yml` workflow automatically:

1. Checks out the [flanksource/base-image](https://github.com/flanksource/base-image) repository
2. Updates the Dockerfile to reference the specific deps version (instead of latest)
3. Creates a pull request with:
   - Version update in the Dockerfile
   - Release notes from the deps release
   - Link to the release

**Requirements:**
- A `FLANKBOT_GITHUB_TOKEN` secret must be configured in the repository with permissions to:
  - Read from flanksource/base-image
  - Create branches on flanksource/base-image
  - Create pull requests on flanksource/base-image

This ensures that base-image is kept up-to-date with the latest tested deps releases.
