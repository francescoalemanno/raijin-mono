# Release Scripts

## release.sh

A local tag creation helper that:
1. Bumps the version number (patch/minor/major)
2. Commits the version change
3. Creates an annotated git tag
4. Pushes the commit and tag to origin
5. Lets the tag-triggered GitHub Actions workflow build binaries, write release notes, and publish the GitHub release

### Prerequisites

- Git configured with access to push to the repository
- Run from repository root: `./scripts/release.sh`
- GitHub Actions enabled for the repository so pushed tags can publish releases

### Usage

```bash
# Patch release (0.1.0 -> 0.1.1) - default
./scripts/release.sh

# Minor release (0.1.0 -> 0.2.0)
./scripts/release.sh minor

# Major release (0.1.0 -> 1.0.0)
./scripts/release.sh major

# Dry run - see what would happen without making changes
./scripts/release.sh patch --dry-run
```

### GitHub Actions Release Workflow

The workflow in `.github/workflows/release.yml` runs when a `v*` tag is pushed. It:
- Runs `go test ./...`
- Builds archives for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, and `windows/arm64`
- Publishes a `SHA256SUMS` manifest
- Generates categorized release notes from conventional commit subjects
- Creates or updates the GitHub release for that tag

### Release Notes Format

Release notes are generated from commit subjects and grouped into:
- **Breaking Changes** (`type!:`)
- **Features** (`feat:`)
- **Bug Fixes** (`fix:`)
- **Performance** (`perf:`)
- **Refactoring** (`refactor:`)
- **Build System** (`build:`)
- **Documentation** (`docs:`)
- **Tests** (`test:`)
- **CI/CD** (`ci:`)
- **Chores** (`chore:`)
- **Code Style** (`style:`)
- **Other** (non-conventional commits)

### Assets

Binary archives and `SHA256SUMS` are uploaded to each GitHub release.

Users install via `scripts/install.sh`, which downloads the latest matching prebuilt archive.

### Safety Checks

The script validates:
- No uncommitted changes exist
- The new tag does not already exist
- All prerequisites are met before making any changes
