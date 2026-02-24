# Release Scripts

## release.sh

A complete local release workflow that:
1. Bumps the version number (patch/minor/major)
2. Commits the version change
3. Creates an annotated git tag
4. Builds binaries for all platforms
5. Generates categorized release notes from commits
6. Creates a GitHub release with all binaries attached
7. Pushes commit and tag to origin

### Prerequisites

- Go 1.24+ installed
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

The script automatically categorizes commits by conventional commit prefix:
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

### What Gets Uploaded

| Platform | Architecture | Binary |
|----------|--------------|--------|
| Linux | amd64 | `raijin-linux-amd64` |
| Linux | arm64 | `raijin-linux-arm64` |
| macOS | amd64 | `raijin-darwin-amd64` |
| macOS | arm64 | `raijin-darwin-arm64` |
| Windows | amd64 | `raijin-windows-amd64.exe` |
| Windows | arm64 | `raijin-windows-arm64.exe` |

### Safety Checks

The script validates:
- No uncommitted changes exist
- The new tag does not already exist
- GitHub CLI is installed and authenticated
- All prerequisites are met before making any changes

### Recovery

If the build fails after commit/tag creation:
```bash
# Revert the commit and delete the tag
git reset --soft HEAD~1
git tag -d v0.1.1
```
