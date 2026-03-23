#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: ./scripts/release-notes.sh --tag <tag> [--repo <owner/name>] [--output <file>]

Generate categorized release notes from commit subjects between the previous tag and
the supplied tag.
EOF
}

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

TAG=""
REPO="${GITHUB_REPOSITORY:-}"
OUTPUT=""

while [ "$#" -gt 0 ]; do
    case "$1" in
        --tag)
            TAG="${2:-}"
            shift 2
            ;;
        --repo)
            REPO="${2:-}"
            shift 2
            ;;
        --output)
            OUTPUT="${2:-}"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

if [ -z "$TAG" ]; then
    echo "--tag is required" >&2
    usage >&2
    exit 1
fi

VERSION="${TAG#v}"
if [ "$VERSION" = "$TAG" ]; then
    echo "tag must start with v (got: $TAG)" >&2
    exit 1
fi

if ! git rev-parse "$TAG" >/dev/null 2>&1; then
    echo "tag does not exist locally: $TAG" >&2
    exit 1
fi

if [ -z "$REPO" ]; then
    remote_url=$(git config --get remote.origin.url || true)
    case "$remote_url" in
        git@github.com:*.git)
            REPO=${remote_url#git@github.com:}
            REPO=${REPO%.git}
            ;;
        https://github.com/*)
            REPO=${remote_url#https://github.com/}
            REPO=${REPO%.git}
            ;;
    esac
fi

PREVIOUS_TAG=$(git describe --tags --abbrev=0 "${TAG}^" 2>/dev/null || true)
if [ -n "$PREVIOUS_TAG" ]; then
    RANGE="${PREVIOUS_TAG}..${TAG}"
else
    RANGE="$TAG"
fi

COMMITS_FILE=$(mktemp)
NOTES_FILE=$(mktemp)
cleanup() {
    rm -f "$COMMITS_FILE" "$NOTES_FILE"
}
trap cleanup EXIT

git log --pretty=format:'%s%x09%h' --no-merges "$RANGE" > "$COMMITS_FILE"
TOTAL_COMMITS=$(grep -c '.' "$COMMITS_FILE" || true)

match_commits() {
    local pattern="$1"
    grep -E "$pattern" "$COMMITS_FILE" || true
}

BREAKING=$(match_commits '^[[:space:]]*(feat|fix|perf|refactor|build|docs|test|ci|chore|style)(\([^)]+\))?!:')
FEATURES=$(match_commits '^[[:space:]]*feat(\([^)]+\))?:')
FIXES=$(match_commits '^[[:space:]]*fix(\([^)]+\))?:')
PERF=$(match_commits '^[[:space:]]*perf(\([^)]+\))?:')
REFACTOR=$(match_commits '^[[:space:]]*refactor(\([^)]+\))?:')
BUILD=$(match_commits '^[[:space:]]*build(\([^)]+\))?:')
DOCS=$(match_commits '^[[:space:]]*docs(\([^)]+\))?:')
TESTS=$(match_commits '^[[:space:]]*test(\([^)]+\))?:')
CI=$(match_commits '^[[:space:]]*ci(\([^)]+\))?:')
CHORE=$(match_commits '^[[:space:]]*chore(\([^)]+\))?:')
STYLE=$(match_commits '^[[:space:]]*style(\([^)]+\))?:')
OTHER=$(grep -Ev '^[[:space:]]*(feat|fix|perf|refactor|build|docs|test|ci|chore|style)(\([^)]+\))?!?:' "$COMMITS_FILE" || true)

format_section() {
    local title="$1"
    local content="$2"
    if [ -z "$content" ]; then
        return
    fi

    {
        echo "### $title"
        while IFS=$'\t' read -r subject hash; do
            [ -z "$subject" ] && continue
            printf -- "- %s (%s)\n" "$subject" "$hash"
        done <<< "$content"
        echo ""
    } >> "$NOTES_FILE"
}

COMPARE_URL=""
if [ -n "$REPO" ] && [ -n "$PREVIOUS_TAG" ]; then
    COMPARE_URL="https://github.com/$REPO/compare/$PREVIOUS_TAG...$TAG"
fi

{
    echo "## Release $TAG"
    echo ""
    echo "- Version: $VERSION"
    echo "- Total commits: $TOTAL_COMMITS"
    if [ -n "$PREVIOUS_TAG" ]; then
        echo "- Previous release: $PREVIOUS_TAG"
    else
        echo "- Previous release: initial release"
    fi
    echo ""
    echo "## What's Changed"
    echo ""
} > "$NOTES_FILE"

format_section "Breaking Changes" "$BREAKING"
format_section "Features" "$FEATURES"
format_section "Bug Fixes" "$FIXES"
format_section "Performance" "$PERF"
format_section "Refactoring" "$REFACTOR"
format_section "Build System" "$BUILD"
format_section "Documentation" "$DOCS"
format_section "Tests" "$TESTS"
format_section "CI" "$CI"
format_section "Chores" "$CHORE"
format_section "Style" "$STYLE"
format_section "Other" "$OTHER"

if [ "$TOTAL_COMMITS" -eq 0 ]; then
    {
        echo "No changes since the previous release."
        echo ""
    } >> "$NOTES_FILE"
fi

{
    echo "## Installation"
    echo ""
    echo "Download the archive for your platform from the release assets below."
    echo "Verify downloads with the published SHA256SUMS file."
    echo ""
    echo "Unix installer:"
    echo ""
    echo '```sh'
    echo 'curl -fsSL https://raw.githubusercontent.com/francescoalemanno/raijin-mono/main/scripts/install.sh | sh'
    echo '```'
    echo ""
    if [ -n "$COMPARE_URL" ]; then
        echo "Full Changelog: $COMPARE_URL"
    fi
} >> "$NOTES_FILE"

if [ -n "$OUTPUT" ]; then
    cp "$NOTES_FILE" "$OUTPUT"
else
    cat "$NOTES_FILE"
fi
