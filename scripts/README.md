# Release Scripts

## release.sh

A complete local release workflow that:
1. Bumps the version number (patch/minor/major)
2. Commits the version change
3. Creates an annotated git tag
4. Generates improved categorized release notes from commits
5. Creates a GitHub release (without binary assets)
6. Pushes commit and tag to origin

### Prerequisites

- Git configured with access to push to the repository
- GitHub CLI (`gh`) installed and authenticated
- Run from repository root: `./scripts/release.sh`

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

### Release Notes Format

The script categorizes commits by conventional commit prefix, with a release summary and install instructions:
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

No binary assets are uploaded to the GitHub release.

Users install via `scripts/install.sh`, which downloads the source for the latest release tag and builds locally with Go.

### Safety Checks

The script validates:
- No uncommitted changes exist
- The new tag does not already exist
- GitHub CLI is installed and authenticated
- All prerequisites are met before making any changes
