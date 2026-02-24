#!/bin/bash

# Release workflow for Raijin
# Usage: ./scripts/release.sh [patch|minor|major] [--dry-run]
# Default: patch

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

VERSION_FILE="internal/version/VERSION"
BUILD_DIR="build"
BUMP_TYPE="${1:-patch}"
DRY_RUN=false

# Check for dry-run flag
for arg in "$@"; do
    if [ "$arg" == "--dry-run" ]; then
        DRY_RUN=true
    fi
done

print_header() {
    echo ""
    echo -e "${CYAN}========================================${NC}"
    echo -e "${CYAN}$1${NC}"
    echo -e "${CYAN}========================================${NC}"
}

# Validate bump type
case "$BUMP_TYPE" in
    patch|minor|major) ;;
    --dry-run) BUMP_TYPE="patch" ;;
    *)
        echo -e "${RED}Error: Invalid bump type '$BUMP_TYPE'${NC}"
        echo "Usage: $0 [patch|minor|major] [--dry-run]"
        exit 1
        ;;
esac

# Check prerequisites
print_header "Checking Prerequisites"

# Check git
if ! command -v git &> /dev/null; then
    echo -e "${RED}Error: git is not installed${NC}"
    exit 1
fi

# Check Go
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

# Check GitHub CLI
if ! command -v gh &> /dev/null; then
    echo -e "${RED}Error: GitHub CLI (gh) is not installed${NC}"
    echo "Install from: https://cli.github.com/"
    exit 1
fi

# Check gh authentication
if ! gh auth status &> /dev/null; then
    echo -e "${RED}Error: Not authenticated with GitHub CLI${NC}"
    echo "Run: gh auth login"
    exit 1
fi

# Get repository info
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || echo "")
if [ -z "$REPO" ]; then
    echo -e "${RED}Error: Could not determine repository. Ensure you're in a git repository with GitHub remote.${NC}"
    exit 1
fi

echo -e "${GREEN}Repository: $REPO${NC}"
echo -e "${GREEN}GitHub CLI: authenticated${NC}"
echo -e "${GREEN}Go version: $(go version | awk '{print $3}')${NC}"

# Get current version
print_header "Version Bump"

CURRENT_VERSION=$(cat "$VERSION_FILE" | tr -d '[:space:]')
echo -e "${BLUE}Current version: $CURRENT_VERSION${NC}"

# Parse version components
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_VERSION"

# Calculate new version
case "$BUMP_TYPE" in
    major)
        NEW_MAJOR=$((MAJOR + 1))
        NEW_MINOR=0
        NEW_PATCH=0
        ;;
    minor)
        NEW_MAJOR=$MAJOR
        NEW_MINOR=$((MINOR + 1))
        NEW_PATCH=0
        ;;
    patch)
        NEW_MAJOR=$MAJOR
        NEW_MINOR=$MINOR
        NEW_PATCH=$((PATCH + 1))
        ;;
esac

NEW_VERSION="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"
TAG="v${NEW_VERSION}"

echo -e "${GREEN}Bump type: $BUMP_TYPE${NC}"
echo -e "${GREEN}New version: $NEW_VERSION${NC}"
echo -e "${GREEN}New tag: $TAG${NC}"

# Check for uncommitted changes
if ! git diff-index --quiet HEAD --; then
    echo -e "${RED}Error: You have uncommitted changes:${NC}"
    git status --short
    echo ""
    echo "Please commit or stash them before releasing."
    exit 1
fi

# Check if tag already exists
if git rev-parse "$TAG" >/dev/null 2>&1; then
    echo -e "${RED}Error: Tag $TAG already exists${NC}"
    exit 1
fi

# Dry run mode
if [ "$DRY_RUN" = true ]; then
    echo ""
    echo -e "${YELLOW}DRY RUN MODE - No changes will be made${NC}"
    echo "Would perform the following actions:"
    echo "  1. Update $VERSION_FILE to $NEW_VERSION"
    echo "  2. Commit: 'chore(release): bump version to $NEW_VERSION'"
    echo "  3. Create tag: $TAG"
    echo "  4. Build binaries for all platforms"
    echo "  5. Create GitHub release with release notes"
    echo "  6. Push commit and tag to origin"
    exit 0
fi

# Confirm
read -p "Proceed with release? [y/N] " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Aborted.${NC}"
    exit 0
fi

# Update VERSION file
print_header "Updating Version"

echo "$NEW_VERSION" > "$VERSION_FILE"
echo -e "${GREEN}Updated $VERSION_FILE to $NEW_VERSION${NC}"

# Stage and commit
git add "$VERSION_FILE"
git commit -m "chore(release): bump version to $NEW_VERSION"
echo -e "${GREEN}Created commit: $(git rev-parse --short HEAD)${NC}"

# Create tag
git tag -a "$TAG" -m "Release $TAG"
echo -e "${GREEN}Created annotated tag: $TAG${NC}"

# Build binaries
print_header "Building Binaries"

# Clean previous builds
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

# Run build script
if ! ./build-all.sh; then
    echo -e "${RED}Build failed!${NC}"
    echo "You may need to revert the commit and delete the tag:"
    echo "  git reset --soft HEAD~1"
    echo "  git tag -d $TAG"
    exit 1
fi

# Verify binaries were created
BINARIES=$(ls -1 "$BUILD_DIR"/raijin-* 2>/dev/null || echo "")
if [ -z "$BINARIES" ]; then
    echo -e "${RED}Error: No binaries found in $BUILD_DIR/${NC}"
    exit 1
fi

echo -e "${GREEN}Built $(echo "$BINARIES" | wc -l | tr -d ' ') binaries${NC}"

# Generate release notes
print_header "Generating Release Notes"

# Get the previous tag for comparison
PREVIOUS_TAG=$(git describe --tags --abbrev=0 "$TAG"^ 2>/dev/null || echo "")

if [ -z "$PREVIOUS_TAG" ]; then
    # First release - show all commits
    COMMITS=$(git log --pretty=format:"- %s" --no-merges)
    COMPARE_URL=""
else
    # Show commits since last tag
    COMMITS=$(git log --pretty=format:"- %s" --no-merges "$PREVIOUS_TAG".."$TAG")
    COMPARE_URL="https://github.com/$REPO/compare/$PREVIOUS_TAG...$TAG"
fi

# Group commits by type
FEATURES=$(echo "$COMMITS" | grep "^- feat" || true)
FIXES=$(echo "$COMMITS" | grep "^- fix" || true)
BUILD=$(echo "$COMMITS" | grep "^- build" || true)
DOCS=$(echo "$COMMITS" | grep "^- docs" || true)
REFACTOR=$(echo "$COMMITS" | grep "^- refactor" || true)
PERF=$(echo "$COMMITS" | grep "^- perf" || true)
TESTS=$(echo "$COMMITS" | grep "^- test" || true)
CHORE=$(echo "$COMMITS" | grep "^- chore" || true)
STYLE=$(echo "$COMMITS" | grep "^- style" || true)
CI=$(echo "$COMMITS" | grep "^- ci" || true)
OTHER=$(echo "$COMMITS" | grep -v "^- feat\|^- fix\|^- build\|^- docs\|^- refactor\|^- perf\|^- test\|^- chore\|^- style\|^- ci" || true)

# Create release notes
RELEASE_NOTES_FILE=$(mktemp)
{
    echo "## What's Changed"
    echo ""

    if [ -n "$FEATURES" ]; then
        echo "### Features"
        echo "$FEATURES"
        echo ""
    fi

    if [ -n "$FIXES" ]; then
        echo "### Bug Fixes"
        echo "$FIXES"
        echo ""
    fi

    if [ -n "$PERF" ]; then
        echo "### Performance Improvements"
        echo "$PERF"
        echo ""
    fi

    if [ -n "$REFACTOR" ]; then
        echo "### Code Refactoring"
        echo "$REFACTOR"
        echo ""
    fi

    if [ -n "$BUILD" ]; then
        echo "### Build System"
        echo "$BUILD"
        echo ""
    fi

    if [ -n "$DOCS" ]; then
        echo "### Documentation"
        echo "$DOCS"
        echo ""
    fi

    if [ -n "$TESTS" ]; then
        echo "### Tests"
        echo "$TESTS"
        echo ""
    fi

    if [ -n "$CI" ]; then
        echo "### CI/CD"
        echo "$CI"
        echo ""
    fi

    if [ -n "$CHORE" ]; then
        echo "### Chores"
        echo "$CHORE"
        echo ""
    fi

    if [ -n "$STYLE" ]; then
        echo "### Code Style"
        echo "$STYLE"
        echo ""
    fi

    if [ -n "$OTHER" ]; then
        echo "### Other Changes"
        echo "$OTHER"
        echo ""
    fi

    if [ -z "$COMMITS" ]; then
        echo "No changes since $PREVIOUS_TAG"
        echo ""
    fi

    echo "## Binaries"
    echo ""
    echo "| Platform | Architecture | Binary |"
    echo "|----------|--------------|--------|"
    ls -1 "$BUILD_DIR" | grep "^raijin-" | while read -r binary; do
        case "$binary" in
            raijin-linux-amd64)     echo "| Linux | amd64 | \`$binary\` |" ;;
            raijin-linux-arm64)     echo "| Linux | arm64 | \`$binary\` |" ;;
            raijin-darwin-amd64)    echo "| macOS | amd64 | \`$binary\` |" ;;
            raijin-darwin-arm64)    echo "| macOS | arm64 | \`$binary\` |" ;;
            raijin-windows-amd64.exe) echo "| Windows | amd64 | \`$binary\` |" ;;
            raijin-windows-arm64.exe) echo "| Windows | arm64 | \`$binary\` |" ;;
        esac
    done
    echo ""
    echo "## Checksums"
    echo ""
    echo '```'
    (cd "$BUILD_DIR" && sha256sum raijin-* 2>/dev/null || shasum -a 256 raijin-* 2>/dev/null || echo "Checksums generated locally")
    echo '```'
    echo ""

    if [ -n "$COMPARE_URL" ]; then
        echo "---"
        echo "**Full Changelog**: $COMPARE_URL"
    fi
} > "$RELEASE_NOTES_FILE"

echo -e "${GREEN}Release notes generated${NC}"

# Push commit and tag before creating release
print_header "Pushing to Remote"

echo -e "${BLUE}Pushing commit and tag to origin...${NC}"
git push origin HEAD
git push origin "$TAG"

echo -e "${GREEN}Pushed to origin${NC}"

# Create GitHub release
print_header "Creating GitHub Release"

echo -e "${BLUE}Creating release $TAG...${NC}"

if ! gh release create "$TAG" \
    --repo "$REPO" \
    --title "$TAG" \
    --notes-file "$RELEASE_NOTES_FILE" \
    "$BUILD_DIR"/*; then
    echo -e "${RED}Failed to create GitHub release${NC}"
    echo "Release notes saved to: $RELEASE_NOTES_FILE"
    exit 1
fi

# Cleanup temp file
rm -f "$RELEASE_NOTES_FILE"

echo -e "${GREEN}Created GitHub release: https://github.com/$REPO/releases/tag/$TAG${NC}"

# Summary
print_header "Release Complete"

echo -e "${GREEN}Version: $NEW_VERSION${NC}"
echo -e "${GREEN}Tag: $TAG${NC}"
echo -e "${GREEN}Release: https://github.com/$REPO/releases/tag/$TAG${NC}"
echo ""
echo -e "${CYAN}Binaries uploaded:${NC}"
ls -lh "$BUILD_DIR"/raijin-* | awk '{printf "  %s (%s)\n", $9, $5}'
