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

- `.github/workflows/auto-release.yml` - Automatic version bumping and tagging on main branch merges
- `.github/workflows/release.yml` - GoReleaser workflow that triggers on tag pushes
- `.goreleaser.yml` - GoReleaser configuration for building and releasing binaries

## Version Calculation

The auto-release workflow uses [svu](https://github.com/caarlos0/svu) to calculate the next version based on:

1. Conventional commit messages since the last tag
2. Current semantic version from the latest tag
3. If no tags exist, starts with v0.1.0

## Disabling Auto-Release

To skip auto-release for a specific merge, you can:

1. Use commit messages that don't trigger version bumps (avoid feat/fix/breaking changes)
2. Or temporarily disable the workflow by adding `[skip ci]` to commit messages