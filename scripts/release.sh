#!/usr/bin/env bash

# Local tag creation helper for the GitHub Actions release workflow.
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

REPO=$(git remote get-url origin 2>/dev/null || echo "")
if [ -z "$REPO" ]; then
    echo -e "${RED}Error: Could not determine repository. Ensure origin is configured.${NC}"
    exit 1
fi

echo -e "${GREEN}Repository remote: $REPO${NC}"

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
    echo "  4. Push commit and tag to origin"
    echo "  5. Let .github/workflows/release.yml build binaries and publish the release"
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

# Push commit and tag
print_header "Pushing to Remote"

echo -e "${BLUE}Pushing commit and tag to origin...${NC}"
git push origin HEAD
git push origin "$TAG"

echo -e "${GREEN}Pushed to origin${NC}"

# Summary
print_header "Release Complete"

echo -e "${GREEN}Version: $NEW_VERSION${NC}"
echo -e "${GREEN}Tag: $TAG${NC}"
echo -e "${GREEN}GitHub Actions will now build assets and publish the release for $TAG${NC}"
