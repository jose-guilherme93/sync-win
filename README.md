# SyncWin

**Self-hosted sync and monitoring for your Linux machines.** Keep small
preference files and game saves in sync, and watch CPU, memory, disk, network,
packages, containers and system logs from one dashboard.

[![CI](https://github.com/jose-guilherme93/sync-win/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/jose-guilherme93/sync-win/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jose-guilherme93/sync-win?sort=semver)](https://github.com/jose-guilherme93/sync-win/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Docker Hub](https://img.shields.io/docker/v/joseguilherme93/sync-win?label=docker%20hub&sort=semver)](https://hub.docker.com/r/joseguilherme93/sync-win)
[![Docker pulls](https://img.shields.io/docker/pulls/joseguilherme93/sync-win)](https://hub.docker.com/r/joseguilherme93/sync-win)
[![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Svelte 5](https://img.shields.io/badge/Svelte-5-FF3E00?logo=svelte&logoColor=white)](https://svelte.dev)

---

## What it does

SyncWin is a server plus a small agent that runs on each Linux machine. The agent
collects an explicit, allowlisted set of files and reports hardware telemetry; the
server stores both and serves a dashboard.

It is deliberately **not** a backup tool and **not** a snapshot system. It moves
small text files and nothing else.

**Sync**

- allowlisted preference files (`~/.bashrc`, KDE/GTK settings, VS Code settings,
  workspace configs)
- game saves from Hydra Launcher (Wine prefixes, Saved Games, AppData), Steam
  userdata and Unity3D, with one-click restore back to a device
- content hashes, so an unchanged file is never re-uploaded

**Monitor**

- live telemetry: CPU (per core), memory, swap, disk and network rates,
  temperatures, power, battery, uptime, load
- installed packages (apt, pacman/AUR, flatpak, AppImage) and pending updates
- systemd services, open ports and Docker containers
- Linux journal logs, searchable and filterable per device
- Lynis security audits with hardening score and history
- alerts, per-device notes and file attachments
- Telegram / webhook / in-app notifications

**Manage** (all opt-in, off by default)

- start, stop, restart, kill and remove containers; exec, compose, prune
- restart the agent, run package updates, reboot a device

---

## Quick start

Requires Docker with the Compose plugin. Nothing else — no checkout, no Go, no
Node.

```bash
# 1. Get this one file and generate the required secret
curl -fsSLO https://raw.githubusercontent.com/jose-guilherme93/sync-win/main/compose.yaml
echo "SYNCWIN_SECRET_KEY=$(openssl rand -hex 32)" > .env

# 2. Start
docker compose up -d
```

Open **http://localhost:8080** and create your account.

That is the whole setup. The first start creates the database inside a named
Docker volume, so your data survives every update.

> **After you create your account**, set `SYNCWIN_ENABLE_REGISTRATION=false` in
> `.env` and run `docker compose up -d` again. Registration is open by default so
> the first account is easy to make; leaving it open on a public server lets
> anyone sign up.

<details>
<summary>Prefer to build from source instead of pulling the image?</summary>

```bash
git clone https://github.com/jose-guilherme93/sync-win.git
cd sync-win
echo "SYNCWIN_SECRET_KEY=$(openssl rand -hex 32)" > .env
docker compose up -d --build
```

Or use `make prod`, which does the same through the Makefile.
</details>

### Next: install an agent

In the dashboard, open **Add device**, copy the generated command and run it on
the Linux machine:

```bash
curl -fsSL http://YOUR-SERVER:8080/install/TOKEN -o /tmp/sync-win-install.sh
sudo bash /tmp/sync-win-install.sh
```

The installer is idempotent: running it again updates the agent in place. It
installs a systemd service, grants the agent the journal group so device logs
work, and enables automatic updates.

---

## Configuration

All settings are environment variables, read from `.env` next to `compose.yaml`.
Only `SYNCWIN_SECRET_KEY` is required.

| Variable | Default | Purpose |
| --- | --- | --- |
| `SYNCWIN_SECRET_KEY` | — (**required**) | Encrypts stored provider credentials. Must stay stable. Generate with `openssl rand -hex 32`. |
| `SYNCWIN_HTTP_PORT` | `8080` | Host port for the dashboard and API. |
| `SYNCWIN_ENABLE_REGISTRATION` | `true` | Open sign-up. Turn off after creating your account. |
| `SYNCWIN_ADMIN_EMAIL` / `SYNCWIN_ADMIN_PASSWORD` | — | Bootstrap the first account from the environment instead of signing up. Created once, never overwritten. |
| `SYNCWIN_SESSION_TTL_HOURS` | `720` | Dashboard session lifetime (30 days). |
| `SYNCWIN_ENABLE_REMOTE_MUTATIONS` | `false` | Allow reboot / package updates / agent restart. |
| `SYNCWIN_ENABLE_DOCKER_MUTATIONS` | `false` | Allow container start/stop/remove, exec, prune. |
| `SYNCWIN_ENABLE_FINGERPRINT_RECONNECT` | `false` | Let a device re-enroll by hardware fingerprint. |
| `SYNCWIN_CORS_ALLOWED_ORIGIN` | — | Comma-separated origins, needed only when the dashboard is served from a different host. |
| `SYNCWIN_HTTPS` | `false` | Set `true` behind TLS so session cookies get the `Secure` flag. |
| `SYNCWIN_DATA_PATH` | named volume | Set to `./data` for a host bind mount instead. |
| `IMAGE_TAG` | `latest` | Pin a release, e.g. `0.6.5`. Recommended for reproducible deploys. |
| `LOG_LEVEL` / `LOG_CONSOLE_LEVEL` | `INFO` | Structured log level. `DEBUG` when investigating. |

Put TLS in front with Caddy, Traefik or nginx. The server speaks plain HTTP and
is designed to sit behind a reverse proxy.

### Updating

```bash
docker compose pull && docker compose up -d
```

The database schema is applied idempotently on every start, so an update needs no
manual migration.

### Backup and restore

Everything lives in the `sync-win-data` volume.

```bash
# Backup
docker run --rm -v sync-win-data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/sync-win-backup.tar.gz -C /data .

# Restore (into a stopped stack)
docker compose down
docker run --rm -v sync-win-data:/data -v "$PWD:/backup" alpine \
  tar xzf /backup/sync-win-backup.tar.gz -C /data
docker compose up -d
```

Keep `SYNCWIN_SECRET_KEY` with the backup. Without it, stored notification
credentials cannot be decrypted.

---

## How it works

```
┌──────────────┐   telemetry, files, saves    ┌─────────────────┐
│  Agent       │ ───────────────────────────► │  Server         │
│  (per host)  │                              │  Go + SQLite    │
│  systemd     │ ◄─────────────────────────── │  serves the     │
└──────────────┘   commands (opt-in)          │  dashboard too  │
                                              └─────────────────┘
```

- **Server** — Go, single binary, SQLite in WAL mode, no external services. Serves
  the REST API and the compiled dashboard from the same origin.
- **Agent** — Go, runs as the unprivileged `sync-win` systemd service. Collects
  an explicit allowlist, applies local policy from `policy.json`, and survives
  server outages with exponential backoff.
- **Dashboard** — Svelte 5 + TypeScript + Vite, served by the Go server, no
  separate web container in production.

The agent never executes arbitrary shell commands. Every privileged action is a
named, validated operation that local policy on the device can refuse.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design and
[COLLECTION-CONTRACT.md](COLLECTION-CONTRACT.md) for exactly what the agent
collects.

---

## Releases and CI

Every push to `main` runs the release pipeline, which derives the next version
from [Conventional Commits](https://www.conventionalcommits.org/):

| Commit | Bump | Example |
| --- | --- | --- |
| `fix:`, `chore:`, anything else | patch | `0.6.4` → `0.6.5` |
| `feat:` | minor | `0.6.5` → `0.7.0` |
| `feat!:`, `BREAKING CHANGE:` | major | `0.7.0` → `1.0.0` |

Versions are never edited by hand. The workflow builds and publishes the image to
Docker Hub — and mirrors it to GHCR — **before** creating the Git tag, so a tag
always has an image to pull. Each release carries SLSA provenance and an SBOM.

Pull a specific version when you want a reproducible deploy:

```bash
IMAGE_TAG=0.6.5 docker compose up -d
```

Every push and pull request runs the full test suite: Go tests with the race
detector, `golangci-lint` on a diff basis, dashboard unit tests, `svelte-check`,
and a browser end-to-end job that boots the real stack and drives a real Chrome.

---

## Development

```bash
make dev          # full stack in containers: server hot reload + web HMR
make test         # server + agent Go tests, dashboard tests, svelte-check
make test-race    # Go tests with the race detector
make lint         # go vet + svelte-check
make help         # every target
```

**Hot reload stack**

| Service | URL | Behaviour |
| --- | --- | --- |
| API | `http://localhost:8088` | Go server rebuilt by [air](https://github.com/air-verse/air) on change |
| Dashboard | `http://localhost:5173` | Vite dev server with HMR |

Development data lives in `./data-dev`, separate from production. Run it
alongside a production stack by changing ports:
`DEV_HTTP_PORT=8081 WEB_PORT=5199 make dev`.

**Without containers**

```bash
make run-server   # API on :8080 against ./data-dev
make run-web      # Vite dev server on :5173
make run-agent    # agent daemon
```

---

## Troubleshooting

**The Logs screen is empty for every device.**
The agent can read only its own journal entries unless it belongs to the
`systemd-journal` group — journal files are `0640 root:systemd-journal`. Check:

```bash
grep Groups /proc/$(pidof sync-win-agent)/status
```

If `systemd-journal` is not listed, re-run the installer, or:

```bash
sudo usermod -aG systemd-journal sync-win
sudo systemctl restart sync-win-agent
```

The dashboard also shows the agent's own reason when log collection fails.

**A device shows as offline.**
Check the agent service and its connection:

```bash
sudo systemctl status sync-win-agent
sudo journalctl -u sync-win-agent -n 100 --no-pager
```

**Agent is not updating itself.**
Updates run from a root-owned systemd timer plus an on-demand path unit:

```bash
systemctl list-timers | grep sync-win
systemctl status sync-win-agent-update.path
```

**Docker mutations do nothing.**
They are off by default. Set `SYNCWIN_ENABLE_DOCKER_MUTATIONS=true` on the server
and check the device's `~/.config/sync-win/policy.json`.

**Forgot the admin password.**
There is no recovery flow. Remove the database row, or set
`SYNCWIN_ADMIN_EMAIL`/`SYNCWIN_ADMIN_PASSWORD` for a fresh account and create a
second one.

---

## Security

- Nothing is stored unencrypted. Provider credentials are encrypted with
  `SYNCWIN_SECRET_KEY` (AES-GCM).
- The agent runs unprivileged with a hardened systemd unit: `NoNewPrivileges`,
  `ProtectSystem=strict`, `PrivateTmp`, `RestrictNamespaces`.
- Collection is allowlist-based. The entire home directory is never copied, and
  secrets, tokens, keyrings and browser credentials are excluded by default.
- Journal lines are redacted for common secret shapes before upload.
- Mutating actions are opt-in per capability and can be refused locally per
  device.
- Sessions use an HttpOnly cookie with CSRF protection.

Found a vulnerability? Please open a security advisory rather than a public
issue.

---

## Contributing

Issues and pull requests are welcome.

- Commits follow [Conventional Commits](https://www.conventionalcommits.org/) —
  the release version is derived from them.
- Run `make test` and `make lint` before opening a PR.
- New collectors and sync behaviour need tests. See [AGENTS.md](AGENTS.md) for
  the conventions this repository follows.

## Documentation

| Document | Contents |
| --- | --- |
| [QUICKSTART.md](QUICKSTART.md) | Step-by-step first run |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Components, responsibilities, data flow |
| [API.md](API.md) | REST API reference |
| [DATA-MODEL.md](DATA-MODEL.md) | Tables and relationships |
| [COLLECTION-CONTRACT.md](COLLECTION-CONTRACT.md) | Exactly what the agent collects and sends |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Deployment platforms, tags, verification |
| [SECURITY.md](SECURITY.md) | Threat model and hardening |
| [ROADMAP.md](ROADMAP.md) | Where this is going |

## License

[MIT](LICENSE). Use it, modify it, ship it.
