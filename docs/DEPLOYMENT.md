# Deploying SyncWin

This document covers running the SyncWin server in production, including the
automated release pipeline and what a deployment platform needs to consume it.

The agent installation is a separate concern; see [INSTALL.md](INSTALL.md).

## What gets published

Every push to `main` runs the `Release` workflow, which:

1. Computes the next semantic version from Conventional Commits
2. Builds and pushes the container image to Docker Hub (and mirrors it to GHCR)
3. **Then** creates the Git tag and the GitHub Release

Step 3 comes after step 2 deliberately. A deployment platform watches the Git
tag, so tagging first would advertise a version whose image does not exist yet
and the deploy would fail on a pull that cannot succeed. Publishing first makes
*a tag exists* imply *a matching image exists*. If the publish fails, no tag is
created and no deploy is triggered.

| Commit prefix | Bump | Example |
| --- | --- | --- |
| `fix:`, `chore:`, anything else | patch | `0.3.0` → `0.3.1` |
| `feat:` | minor | `0.3.1` → `0.4.0` |
| `feat!:`, `BREAKING CHANGE:` | major | `0.4.0` → `1.0.0` |

## Two deployment paths, both always available

The release publishes an image **and** the repository keeps a buildable
`Dockerfile.server`. Both paths stay working, because a platform may trigger
from the GitHub tag and build, or from the registry tag and pull.

| Path | Trigger | How |
| --- | --- | --- |
| Build from source | GitHub tag / push | Platform reads `docker/Dockerfile.server` and builds |
| Pull the image | Registry tag on Docker Hub | Platform pulls `docker.io/joseguilherme93/sync-win:<tag>` |

`compose.prod.yaml` supports both from the same file: it carries an `image:`
reference **and** a `build:` section, so Compose builds when asked to and pulls
when asked to.

```bash
make prod          # path A: build locally and start
make prod-pull     # path B: pull the published image and start
make prod-verify   # print the image reference that would be used
```

Pinning the version is what makes a deploy reproducible. `IMAGE_TAG` defaults
to `latest` for convenience, but a real deployment should set it explicitly:

```bash
IMAGE_TAG=0.6.4 \
SYNCWIN_SECRET_KEY=... \
docker compose -f compose.prod.yaml up -d
```

## The image

```
docker.io/joseguilherme93/sync-win:<version>     # 0.6.4
docker.io/joseguilherme93/sync-win:v<version>    # v0.6.4 — same digest
docker.io/joseguilherme93/sync-win:latest
```

Every release is also mirrored to `ghcr.io/jose-guilherme93/sync-win` with the
same three tags. The mirror exists because it needs no credentials at all: the
Release workflow authenticates there with the automatic `GITHUB_TOKEN`, so a
rotated or missing Docker Hub secret cannot leave a tag pointing at an image
that was never published. Pull from either — the digests are identical.

All three tags point at the same digest. Both the bare and the `v`-prefixed
form are published because Docker convention drops the `v` while the Git tag
keeps it: a platform deriving the image tag from either the Git ref or a
registry tag list resolves either way. `latest` moves on every release, so a
registry watcher configured on `latest` deploys each release and one pinned to a
version stays put.

Images are `linux/amd64` only, which is what the deployment target runs. They
carry an SLSA provenance attestation and an SBOM. The runtime image is
multi-stage and the server process runs as an unprivileged user (uid 10001);
the container entrypoint starts as root only long enough to adopt a data
directory created by an older root-running image, then drops privileges.

## Authentication

The workflow authenticates to GHCR with the automatic `GITHUB_TOKEN`, so **no
registry password is stored in this repository**. That is the reason GHCR is
used instead of Docker Hub.

The Docker Hub package is public, so a platform can pull it with no
credentials. If you publish your own build instead, configure credentials:

1. For a private GHCR package, create a personal access token (classic) with
   `read:packages`, or a fine-grained token with *Packages: read*.
2. In the platform's registry settings add:
   - URL: `docker.io` (or `ghcr.io` for the mirror)
   - username: the registry account that owns the image
   - password: the access token, or the Docker Hub token
3. Point the service at `docker.io/joseguilherme93/sync-win:<tag>`.

## Deploying with a Compose file

`compose.prod.yaml` works for both paths. The `image:` default is the Docker Hub
reference and the `build:` section is kept, so:

- a platform that only pulls needs no local build
- `make prod` still builds from a checkout with no registry access

A platform that only pulls can remove the `build:` section; leaving it in place
is harmless because Compose builds only when asked.

`SYNCWIN_SECRET_KEY` is required (the Compose file enforces it). Changing it
makes previously stored notification credentials undecryptable.

## Upgrading

Data lives in the `sync-win-data` volume and survives redeploys.

Upgrading across the change to an unprivileged runtime is handled by the
entrypoint, which takes ownership of a data directory left by an older
root-running image. Without that step the server fails to open an existing
database with:

```
new store: ping sqlite: attempt to write a readonly database (1544)
```

If a deployment bypasses the image entrypoint and starts `/app/server`
directly, it must `chown -R 10001:10001 /data` first.

## Verifying a release

After a release run, three things should be true together:

```bash
# 1. the tag exists
git fetch --tags && git tag --list 'v*' --sort=-v:refname | head -3

# 2. it points at the deployed commit
git rev-list -n1 v0.6.4

# 3. the image exists
docker pull docker.io/joseguilherme93/sync-win:0.6.4
```

If the tag exists but the image does not, the ordering invariant has been
broken and the workflow needs looking at.

## Architecture support

The release builds `linux/amd64` only.

Adding `linux/arm64` to the platform list looks harmless and is not: GitHub's
amd64 runners have no arm64 hardware, so buildx runs the arm64 stages under
QEMU emulation. The Go stages survive that (Go cross-compiles natively with
`CGO_ENABLED=0`), but the web stage runs `npm ci` under emulation and takes over
20 minutes on its own. A release took 28 minutes and never finished before this
was reverted.

If arm64 is ever needed, do not add the platform to the build. Either:

1. Build the dashboard once on the runner and only cross-compile the Go
   binaries per platform, or
2. Use a native arm64 runner (GitHub's `ubuntu-24.04-arm`) and join the images
   with `docker buildx imagetools create`.

Both avoid emulating Node, whose output is a static asset bundle that does not
depend on the target architecture at all.

`TARGETOS`/`TARGETARCH` are already threaded through `docker/Dockerfile.server`,
so either path works without further changes.