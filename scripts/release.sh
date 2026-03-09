#!/bin/bash

# Release workflow for Raijin
# Usage: ./scripts/release.sh [patch|minor|major] [--dry-run]
# Default: patch

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

VERSION_FILE="internal/version/VERSION"
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

if ! command -v git &> /dev/null; then
    echo -e "${RED}Error: git is not installed${NC}"
    exit 1
fi

if ! command -v gh &> /dev/null; then
    echo -e "${RED}Error: GitHub CLI (gh) is not installed${NC}"
    echo "Install from: https://cli.github.com/"
    exit 1
fi

if ! gh auth status &> /dev/null; then
    echo -e "${RED}Error: Not authenticated with GitHub CLI${NC}"
    echo "Run: gh auth login"
    exit 1
fi

REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || echo "")
if [ -z "$REPO" ]; then
    echo -e "${RED}Error: Could not determine repository. Ensure you're in a git repository with GitHub remote.${NC}"
    exit 1
fi

echo -e "${GREEN}Repository: $REPO${NC}"
echo -e "${GREEN}GitHub CLI: authenticated${NC}"

# Get current version
print_header "Version Bump"

CURRENT_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
echo -e "${BLUE}Current version: $CURRENT_VERSION${NC}"

IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_VERSION"

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

if [ "$DRY_RUN" = true ]; then
    echo ""
    echo -e "${YELLOW}DRY RUN MODE - No changes will be made${NC}"
    echo "Would perform the following actions:"
    echo "  1. Update $VERSION_FILE to $NEW_VERSION"
    echo "  2. Commit: 'chore(release): bump version to $NEW_VERSION'"
    echo "  3. Create tag: $TAG"
    echo "  4. Generate improved categorized release notes"
    echo "  5. Push commit and tag to origin"
    echo "  6. Create GitHub release (no binary assets uploaded)"
    exit 0
fi

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

# Generate release notes
print_header "Generating Release Notes"

PREVIOUS_TAG=$(git describe --tags --abbrev=0 "$TAG"^ 2>/dev/null || echo "")

if [ -z "$PREVIOUS_TAG" ]; then
    RANGE="$TAG"
    COMPARE_URL=""
else
    RANGE="$PREVIOUS_TAG..$TAG"
    COMPARE_URL="https://github.com/$REPO/compare/$PREVIOUS_TAG...$TAG"
fi

COMMITS_FILE=$(mktemp)
RELEASE_NOTES_FILE=$(mktemp)

cleanup() {
    rm -f "$COMMITS_FILE" "$RELEASE_NOTES_FILE"
}
trap cleanup EXIT

git log --pretty=format:'%s' --no-merges $RANGE > "$COMMITS_FILE"
TOTAL_COMMITS=$(grep -c '.' "$COMMITS_FILE" || true)

# Category filters (conventional commits + aliases)
BREAKING=$(grep -E '^[[:space:]]*(feat|fix|perf|refactor|build|docs|test|ci|chore|style)(\([^)]+\))?!:' "$COMMITS_FILE" || true)
FEATURES=$(grep -E '^[[:space:]]*feat(\([^)]+\))?:' "$COMMITS_FILE" || true)
FIXES=$(grep -E '^[[:space:]]*fix(\([^)]+\))?:' "$COMMITS_FILE" || true)
PERF=$(grep -E '^[[:space:]]*perf(\([^)]+\))?:' "$COMMITS_FILE" || true)
REFACTOR=$(grep -E '^[[:space:]]*refactor(\([^)]+\))?:' "$COMMITS_FILE" || true)
BUILD=$(grep -E '^[[:space:]]*build(\([^)]+\))?:' "$COMMITS_FILE" || true)
DOCS=$(grep -E '^[[:space:]]*docs(\([^)]+\))?:' "$COMMITS_FILE" || true)
TESTS=$(grep -E '^[[:space:]]*test(\([^)]+\))?:' "$COMMITS_FILE" || true)
CI=$(grep -E '^[[:space:]]*ci(\([^)]+\))?:' "$COMMITS_FILE" || true)
CHORE=$(grep -E '^[[:space:]]*chore(\([^)]+\))?:' "$COMMITS_FILE" || true)
STYLE=$(grep -E '^[[:space:]]*style(\([^)]+\))?:' "$COMMITS_FILE" || true)
OTHER=$(grep -Ev '^[[:space:]]*(feat|fix|perf|refactor|build|docs|test|ci|chore|style)(\([^)]+\))?!?:' "$COMMITS_FILE" || true)

format_section() {
    section_title="$1"
    section_content="$2"

    if [ -n "$section_content" ]; then
        echo "### $section_title" >> "$RELEASE_NOTES_FILE"
        echo "$section_content" | sed -E 's/^[[:space:]]*/- /' >> "$RELEASE_NOTES_FILE"
        echo "" >> "$RELEASE_NOTES_FILE"
    fi
}

{
    echo "## Release $TAG"
    echo ""
    echo "- **Version**: $NEW_VERSION"
    echo "- **Total commits**: $TOTAL_COMMITS"
    if [ -n "$PREVIOUS_TAG" ]; then
        echo "- **From**: $PREVIOUS_TAG"
    else
        echo "- **From**: initial release"
    fi
    echo ""
    echo "## What's Changed"
    echo ""
} > "$RELEASE_NOTES_FILE"

format_section "⚠️ Breaking Changes" "$BREAKING"
format_section "🚀 Features" "$FEATURES"
format_section "🐛 Bug Fixes" "$FIXES"
format_section "⚡ Performance" "$PERF"
format_section "♻️ Refactoring" "$REFACTOR"
format_section "🏗️ Build System" "$BUILD"
format_section "📝 Documentation" "$DOCS"
format_section "✅ Tests" "$TESTS"
format_section "🔁 CI/CD" "$CI"
format_section "🧹 Chores" "$CHORE"
format_section "🎨 Style" "$STYLE"
format_section "📦 Other" "$OTHER"

if [ "$TOTAL_COMMITS" -eq 0 ]; then
    {
        echo "No changes since previous release."
        echo ""
    } >> "$RELEASE_NOTES_FILE"
fi

{
    echo "## Installation"
    echo ""
    echo "Install using the official installer (builds from source of this release tag):"
    echo ""
    echo '```sh'
    echo 'curl -fsSL https://raw.githubusercontent.com/francescoalemanno/raijin-mono/main/scripts/install.sh | sh'
    echo '```'
    echo ""
    if [ -n "$COMPARE_URL" ]; then
        echo "---"
        echo "**Full Changelog**: $COMPARE_URL"
    fi
} >> "$RELEASE_NOTES_FILE"

echo -e "${GREEN}Release notes generated${NC}"

# Push commit and tag
print_header "Pushing to Remote"

echo -e "${BLUE}Pushing commit and tag to origin...${NC}"
git push origin HEAD
git push origin "$TAG"

echo -e "${GREEN}Pushed to origin${NC}"

# Create GitHub release (without binary upload)
print_header "Creating GitHub Release"

echo -e "${BLUE}Creating release $TAG...${NC}"

if ! gh release create "$TAG" \
    --repo "$REPO" \
    --title "$TAG" \
    --notes-file "$RELEASE_NOTES_FILE"; then
    echo -e "${RED}Failed to create GitHub release${NC}"
    exit 1
fi

echo -e "${GREEN}Created GitHub release: https://github.com/$REPO/releases/tag/$TAG${NC}"

# Summary
print_header "Release Complete"

echo -e "${GREEN}Version: $NEW_VERSION${NC}"
echo -e "${GREEN}Tag: $TAG${NC}"
echo -e "${GREEN}Release: https://github.com/$REPO/releases/tag/$TAG${NC}"
echo -e "${GREEN}Assets uploaded: none${NC}"
