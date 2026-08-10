#!/usr/bin/env bash
# Bump the patch version in VERSION, commit, tag vX.Y.Z, and push to main.
#
# Mirrors yatfa's script/bump-version.sh (VERSION file, not package.json — this
# is a Go repo) and warden's tag step (the GitHub Release attaches its assets to
# the tag this creates). Differences for this repo:
#   - no `prod` deploy-mirror branch: nothing here is deployed to a cluster.
#     A release IS the tag plus its uploaded artifacts, so the push to prod that
#     yatfa does is dropped.
#   - the tag is load-bearing rather than informational: scripts/install.sh
#     resolves artifacts from a release URL ending in the tag, and the version it
#     names must equal the one stamped into the binaries by
#     scripts/build-release.sh <semver>, which the release workflow calls with
#     the value this script prints.
#
# Strategy carried over verbatim: must be on main with a clean tree, patch-only
# bump, commit "bump: X -> Y", pull --rebase + push with up to 3 retries.
#
# When run inside GitHub Actions (GITHUB_OUTPUT set), exports `version` and
# `tag` to the step's job outputs. Locally it just prints the new version.
set -e

# Ensure we're on the main branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$CURRENT_BRANCH" != "main" ]; then
    echo "Error: Not on main branch. Current branch: $CURRENT_BRANCH"
    exit 1
fi

# Check if working directory is clean
if [ -n "$(git status --porcelain)" ]; then
    echo "Error: Working directory has uncommitted changes"
    exit 1
fi

# Read current version
if [ ! -f VERSION ]; then
    echo "Error: VERSION file not found"
    exit 1
fi

CURRENT_VERSION=$(tr -d '[:space:]' < VERSION)
echo "Current version: $CURRENT_VERSION"

# Bump patch version (0.1.0 -> 0.1.1)
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_VERSION"
NEW_VERSION="$MAJOR.$MINOR.$((PATCH + 1))"
echo "New version: $NEW_VERSION"

# Refuse to reuse a tag. The release workflow uploads assets to vX.Y.Z, and a
# second release on an existing tag would either fail late (after a full
# cross-compile) or silently attach a second set of binaries to an old release.
if git rev-parse -q --verify "refs/tags/v$NEW_VERSION" >/dev/null || \
   git ls-remote --exit-code --tags origin "refs/tags/v$NEW_VERSION" >/dev/null 2>&1; then
    echo "Error: tag v$NEW_VERSION already exists — VERSION and the tags disagree"
    exit 1
fi

# Update VERSION file
echo "$NEW_VERSION" > VERSION

# Create commit
git add VERSION
git commit -m "bump: $CURRENT_VERSION -> $NEW_VERSION"

# Tag the release. The GitHub Release attaches the four cross-compiled binaries
# and their SHA256SUMS to this tag, and scripts/install.sh downloads from it.
git tag "v$NEW_VERSION"

# Pull with rebase to incorporate any commits that landed on main since checkout,
# then push the branch and the tag. Retry up to 3 times to handle concurrent pushes.
MAX_RETRIES=3
RETRY_COUNT=0
PUSH_SUCCESS=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if git pull --rebase origin main; then
        if git push origin main && git push origin "v$NEW_VERSION"; then
            PUSH_SUCCESS=true
            break
        fi
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    if [ $RETRY_COUNT -lt $MAX_RETRIES ]; then
        echo "Push failed, retrying ($RETRY_COUNT/$MAX_RETRIES)..."
        sleep 5
    fi
done

if [ "$PUSH_SUCCESS" = false ]; then
    echo "Error: Failed to push version bump after $MAX_RETRIES attempts"
    exit 1
fi

echo "Version bumped to $NEW_VERSION and pushed to main with tag v$NEW_VERSION"

# Export to GitHub Actions job outputs when run in CI
if [ -n "$GITHUB_OUTPUT" ]; then
    echo "version=$NEW_VERSION" >> "$GITHUB_OUTPUT"
    echo "tag=v$NEW_VERSION" >> "$GITHUB_OUTPUT"
fi
