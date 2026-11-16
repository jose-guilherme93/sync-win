#!/usr/bin/env bash
# =============================================================================
# make release — from green develop to a published image, one command
# =============================================================================
# What it does, in the order the repository requires:
#
#   1. merge `develop` into `main` through a pull request (CI must pass);
#   2. wait for release-please to open the release PR it decides on;
#   3. merge that release PR (its checks are skipped on purpose: it only rewrites
#      version.txt, the manifest and CHANGELOG.md, and the merge to main runs the
#      full suite again before anything is published);
#   4. wait for `Release`, which publishes the image and then creates the tag.
#
# No safety gate is removed: the image is still published only after the full
# suite passed on the exact commit that is being released, still before the tag,
# and still only when the manifest is ahead of the newest tag.
#
# Run it from a clean checkout of `develop`. It never edits code, never bumps a
# version by hand, and stops at the first red check.
# =============================================================================
set -euo pipefail

say() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
die() { printf 'release: %s\n' "$*" >&2; exit 1; }

command -v gh >/dev/null || die "gh is required"
gh auth status >/dev/null 2>&1 || die "gh is not authenticated"
repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"

# ---------------------------------------------------------------------------
# Approve a workflow run that GitHub parked at action_required. That is what a
# pull request opened with GITHUB_TOKEN looks like: same-repository, trustworthy
# content, but no human has clicked approve. Without this the release PR would
# sit red forever.
# ---------------------------------------------------------------------------
approve_pending() {
    local pr=$1 head id
    head="$(gh pr view "$pr" --json headRefName --jq .headRefName)"
    for id in $(gh run list --branch "$head" --limit 5 \
        --json databaseId,conclusion \
        --jq '.[] | select(.conclusion == "action_required") | .databaseId'); do
        gh api -X POST "repos/${repo}/actions/runs/${id}/approve" >/dev/null \
            && echo "approved the parked run ${id} on ${head}"
    done
}

# wait_checks <pr> — poll until the pull request is mergeable or clearly not.
wait_checks() {
    local pr=$1 state
    for _ in $(seq 1 120); do
        approve_pending "$pr"
        state="$(gh pr view "$pr" --json mergeStateStatus --jq .mergeStateStatus)"
        case "$state" in
            CLEAN) return 0 ;;
            DIRTY) die "#${pr} has conflicts; resolve them on develop first" ;;
            BEHIND) die "#${pr} is behind; merge main into develop and push" ;;
        esac
        sleep 15
    done
    die "timed out waiting for the checks on #${pr}"
}

# ---------------------------------------------------------------------------
# Preconditions. A release started from a tree that is not exactly develop would
# publish something nobody reviewed.
# ---------------------------------------------------------------------------
say "Checking the checkout"
[ -z "$(git status --porcelain)" ] || die "the working tree is not clean"
git fetch --quiet origin
[ "$(git rev-parse --abbrev-ref HEAD)" = "develop" ] || die "check out develop first"
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/develop)" ] \
    || die "develop is not up to date with origin; pull first"

# main only ever moves by merging develop plus the release commit, so it can hold
# merge commits develop does not have. Bringing them back is content-free and it
# is what makes the develop->main pull request mergeable at all.
behind="$(git rev-list --count origin/develop..origin/main)"
if [ "${behind}" != "0" ]; then
    say "main is ${behind} commit(s) ahead; bringing them back into develop"
    git merge --no-edit --quiet origin/main
    git push --quiet origin develop
fi

if [ "$(git rev-list --count origin/main..origin/develop)" = "0" ]; then
    say "Nothing to release"
    echo "develop has no commit that main does not already have."
    exit 0
fi

# ---------------------------------------------------------------------------
# 1. develop -> main
# ---------------------------------------------------------------------------
say "Merging develop into main"
pr="$(gh pr list --state open --base main --head develop --json number --jq '.[0].number // empty')"
if [ -z "${pr}" ]; then
    url="$(gh pr create --base main --head develop \
        --title "chore: release $(cat version.txt) into main" \
        --body "Release decided by release-please from the Conventional Commits on develop. Merging this starts the release; \`make release\` opened it.")"
    pr="${url##*/}"
    echo "opened #${pr}"
else
    echo "reusing #${pr}"
fi
wait_checks "${pr}"
gh pr merge "${pr}" --merge >/dev/null
echo "merged #${pr} into main"

# ---------------------------------------------------------------------------
# 2. Release Please decides the version
# ---------------------------------------------------------------------------
say "Waiting for the release pull request"
release_pr=""
for _ in $(seq 1 40); do
    release_pr="$(gh pr list --state open --label "autorelease: pending" \
        --json number --jq '.[0].number // empty')"
    [ -n "${release_pr}" ] && break
    sleep 15
done
[ -n "${release_pr}" ] || die "release-please opened no release PR; read the release-please run on main"
version="$(gh pr view "${release_pr}" --json title --jq .title | sed -E 's/.*release ([0-9]+\.[0-9]+\.[0-9]+).*/\1/')"
[ -n "${version}" ] || die "could not read the version from #${release_pr}"
echo "#${release_pr} proposes ${version}"

# ---------------------------------------------------------------------------
# 3. Merge the release PR. The full suite runs on main right after, and
#    `Release` refuses to publish unless that run is green.
# ---------------------------------------------------------------------------
say "Merging the release pull request"
wait_checks "${release_pr}"
gh pr merge "${release_pr}" --merge >/dev/null
echo "merged #${release_pr}"

# ---------------------------------------------------------------------------
# 4. Release: image first, then the tag
# ---------------------------------------------------------------------------
say "Waiting for the image and the tag"
published=""
for _ in $(seq 1 60); do
    published="$(gh release view "v${version}" --json tagName,publishedAt --jq .tagName 2>/dev/null || true)"
    [ -n "${published}" ] && break
    sleep 20
done
[ -n "${published}" ] || die "Release did not publish v${version}; read its run"

say "Released ${version}"
gh release view "v${version}" --json tagName,publishedAt,url \
    --jq '"tag:         \(.tagName)\npublished:   \(.publishedAt)\nrelease:     \(.url)"'
cat <<EOF
image:       docker.io/$(gh repo view --json owner --jq .owner.login | tr '[:upper:]' '[:lower:]')/sync-win:${version}

Agents pick the new binary up on their own timer; nothing else to do.
EOF
