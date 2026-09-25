# Roadmap

## P0 — Remediação de segurança (concluída)
- [x] flags fail-closed e validações de entrada
- [x] autorização estrita por sessão e isolamento entre owners
- [x] Argon2id, hashes de tokens, cookie HttpOnly, CSRF e revogação
- [x] rate limiting e proteção SSRF/webhook
- [x] política local fail-closed, limites Docker, timeouts e resultados vinculados ao device
- [x] testes de regressão de segurança e documentation alinhada

A próxima etapa é P1 (funcionalidade), não parte desta remediação.

## P1.1 — Preference sync (concluída)
- [x] conectar o collector ao ciclo do agent
- [x] usar somente o contrato e a allowlist explícita
- [x] enviar preferências ao endpoint autenticado do servidor
- [x] retry sem avançar hashes antes do sucesso
- [x] persistir rejeições e estado de sincronização
- [x] testes de retry, rejeição, allowlist e arquivos inalterados

## P1.2 — Inventário de apps (concluída)
- [x] coletar APT, Flatpak, Pacman, AUR e AppImages pelo contrato
- [x] enviar o inventário autenticado a cada 5 minutos
- [x] preservar o último inventário em falhas de coleta ou upload
- [x] alinhar o payload do servidor ao campo `path`
- [x] testar fontes, filtragem, symlinks, payload e retry

## P1.3 — Saves e restore (concluída)
- [x] coletar as raízes e extras definidos pelo contrato
- [x] aplicar extensões, exclusões, profundidade e limites por ciclo
- [x] sincronizar texto e binários em base64 com retry
- [x] manter estado/hashes separados de preferências
- [x] filtrar restores por prefixo e jogo
- [x] transportar payload seguro no comando de restore
- [x] aplicar política local, feature flag e paths seguros no agent
- [x] escrita atômica com rejeição de traversal e symlink
- [x] testes de collector, encoding, chunking, persistência e restore

## P1.4 — Auto-update do agent (concluída)
- [x] comando `lem-agent update` com comparação de versão
- [x] download por canal autenticado do servidor e verificação SHA-256
- [x] validação da versão baixada antes da troca
- [x] troca atômica com backup e rollback
- [x] timer systemd de 15 minutos com opt-out `LEM_AUTO_UPDATE=0`

A funcionalidade P1 está concluída. A próxima etapa é uma nova priorização.

## FASE 0 — Arquitetura e contratos
- [x] definir o modelo de preferências por dispositivo
- [x] ajustar a API inicial para sync de arquivos pequenos
- [x] documentar regras de coleta e segurança
- [x] definir o contrato de status online e last sync

## FASE 1 — Server mínimo
- [x] Go
- [x] SQLite (modernc.org/sqlite, WAL, schema em initSchema, import automático do estado JSON legado)
- [x] HTTP server
- [x] health endpoint
- [x] upload de pequenos arquivos de texto
- [x] persistence em pasta local /data
- [x] graceful shutdown
- [x] logging

## FASE 2 — Device Registry
- [x] register
- [x] heartbeat
- [x] device ID
- [x] device token
- [x] online/offline
- [x] last_seen_at
- [x] last_sync_at

## FASE 3 — Agent Core
- [x] CLI
- [x] sync loop
- [x] file allowlist
- [x] local validation
- [x] metadata submission
- [x] small file transport

## FASE 4 — Preference Sync
- [x] coletar arquivos pequenos de texto
- [x] mapear por categoria
- [x] armazenar conteúdo textual em arquivos JSON ou TXT
- [x] registrar time de sincronização
- [x] registrar status de sucesso/falha

## FASE 5 — Web Dashboard MVP
- [x] dashboard
- [x] devices
- [x] status online
- [x] last sync
- [x] basic metadata
- [x] small preference list

## FASE 6 — Device Detail
- [x] overview
- [x] device metadata
- [x] preference files
- [x] sync status
- [x] activity log
- [x] last sync time

## FASE 7 — Sync Safety
- [x] allowlist
- [x] reject secrets
- [x] reject binaries
- [x] file size limits
- [x] invalid content filtering

## FASE 8 — User Preference Categories
- [x] desktop settings
- [x] shell preferences
- [x] app config files
- [ ] workspace config
- [x] KDE preferences

## FASE 9 — Device Health
- [x] online/offline summary
- [x] sync failures
- [x] retry states
- [x] stale devices
- [x] alert visibility in UI

## FASE 10 — Observability
- [x] structured logs
- [x] health endpoints
- [x] disk usage
- [x] DB size
- [x] sync event audit
- [x] device status health
- [x] telemetry history (raw + downsampled: 1m/5m/1h)
- [x] HTTP access aggregation
- [x] application metrics
- [x] retention settings per owner

## FASE 11 — Security
- [x] device tokens
- [x] HTTPS
- [x] validation
- [x] rate limiting
- [x] revocation
- [x] secret redaction
- [x] contrato de coleta explícito: `COLLECTION-CONTRACT.md` + `agent/internal/contract/contract.json` embutido no binário (fonte única das constantes, validada por testes)
- [x] política local do agent (`~/.config/lem/policy.json`): o agent recusa comandos desabilitados localmente e reporta a recusa ao servidor — o servidor nunca força execução
- [x] comandos com timeout e cap de saída; pacotes validados antes de executar
- [x] dashboard account-first: tela inicial é login/criar conta; todo device é registrado sob a conta (`owner_id` = usuário autenticado), sem depender de identidade anônima do navegador; `GET /api/auth/me` restaura a sessão no carregamento
- [x] instalador com passos numerados e verificação real de conexão (heartbeat) antes de declarar sucesso; falha imprime checklist acionável
- [x] senhas armazenadas como Argon2id; tokens de sessão/device armazenados apenas como hash; dashboard usa cookie HttpOnly + CSRF; owner isolation strict
- [x] tokens de enrollment: uso único, expiração em 15 minutos, vinculados ao owner
- [x] verificação de integridade do binário via SHA-256 checksum no install
- [x] Lynis security audit: agent runs `lynis audit system --cronjob`, parses report to structured JSON, stores in SQLite with full history. Dashboard shows hardening index gauge, warnings/suggestions, category breakdown

## FASE 11.5 — Agent Resilience
- [x] backoff exponencial com jitter (~10%) em falhas consecutivas de ciclo
- [x] estado persistente sobrevive a restarts (hashes + último sync); JSON corrompido é descartado com log
- [x] hashes de arquivos removidos são podados após sync bem-sucedido (estado não cresce infinito)
- [x] `exclude_file` idempotente (crash entre execução e relatório não duplica linha)
- [x] sync tolerante: arquivo individual ruim é pulado com log; aborta só se nenhum payload válido
- [x] limite de upload usa min(limite local, limite espelhado do server) — nada é enviado sabidamente rejeitável
- [x] E2E real: agent daemon sobrevive à queda do servidor (backoff visível) e se recupera sozinho ("connection recovered") sem re-enviar arquivos inalterados
- [x] recovery de credencial exige novo enrollment token; fingerprint reconexão foi desabilitada por não ser autenticador

## FASE 12 — Save Game Sync
- [x] Contract v1.1.0: categoria `saves` com roots, extensões, filtros e limites
- [x] Agent collector: descobre wine prefixes do Hydra Launcher, `Saved Games`, `AppData/Roaming`, `ludusavi/config.yaml`, Steam userdata, Unity3D
- [x] Base64 encoding para saves binários; chunked upload ≤1.5 MiB/request
- [x] Server validation: 1 MiB/file, skip secret scan para base64, mirror write decodificado
- [x] Sync config endpoints: GET/PUT `/api/sync-config` (owner), GET `/api/devices/{id}/sync-config` (device token)
- [x] Device summary: `saves_count`, `saves_size_bytes`, `saves_last_synced_at`
- [x] Web UI: Settings modal (built-in + extra dirs), device badge, saves tab

## FASE 13 — Packaging and Deployment
- [x] Docker image (multi-stage: Go + Node -> Alpine)
- [x] compose setup
- [x] non-root user
- [x] minimal image
- [x] local persistence
- [x] servidor entrega o dashboard de produção (mesma origem/porta, `LEM_WEB_DIR`, SPA fallback; imagem multi-stage node+go) — elimina o fluxo dev-server + CORS que deixava a UI lenta
- [x] inventário de pacotes renderizado em grupos limitados (busca filtra tudo; "Show all" expande), abas com carregamento preguiçoso e cache de 60s — cliques suaves com milhares de pacotes

## FASE 14 — Docker Monitoring and Management
- [x] Agent Docker socket reader (`agent/collectors/docker.go`) — container listing, stats, logs, info
- [x] 21 Docker command types in contract.json (list, stats, logs, start/stop/restart/kill/remove, exec, prune, compose read/write/up/down/ps/logs)
- [x] Agent command handlers for all Docker operations
- [x] Server Docker API endpoints (list, action, exec, compose, prune, result)
- [x] Database migration for command payload column
- [x] DockerTab.svelte component with container list, actions, compose editor, exec, prune
- [x] DeviceModal Docker tab integration with lazy loading
- [x] Docker summary in hardware telemetry (docker_available, containers, info)
- [x] Security: container ID validation, compose path validation, audit logging, confirmation dialogs

## FASE 15 — Device Notes and Attachments
- [x] Database migration for device_notes and device_attachments tables
- [x] Server API endpoints for CRUD operations on notes and attachments
- [x] DeviceModal Notes tab with inline note editing
- [x] File attachment upload and display with caption support

## FASE 16 — Notification System
- [x] Notification provider interface (`server/internal/notify/provider.go`)
- [x] Provider registry with auto-registration via `init()`
- [x] Telegram provider implementation
- [x] Webhook provider implementation
- [x] Web inbox provider (in-app notifications)
- [x] Dispatcher with 15-minute throttle window per event/device
- [x] Status watcher: detects online/offline transitions and emits events
- [x] SSE broadcaster for real-time notification push to browsers
- [x] AES-GCM encryption for provider credentials
- [x] Notification settings UI (NotificationsModal.svelte)
- [x] Notification inbox with mark-as-read
- [x] Toast notifications (NotificationToast.svelte)
- [x] Telegram chat detection endpoint

## FASE 17 — Logging and Observability
- [x] Structured JSON logger (`server/internal/logging/logger.go`)
- [x] Secret redaction in logs (`server/internal/logging/redact.go`)
- [x] Log deduplication (`server/internal/logging/dedup.go`)
- [x] HTTP request logging middleware
- [x] Server-side log persistence in SQLite
- [x] HTTP access aggregation for request performance metrics
- [x] Application metrics time-series
- [x] Log retention settings per owner
- [x] Dashboard log viewer with filtering by level, device, and time range

## FASE 18 — Hardware Telemetry
- [x] Agent collectors: CPU, memory, disk I/O, network, temperatures, battery, disk partitions, swap, top processes
- [x] Agent self-impact measurement (CPU and memory usage)
- [x] Hardware fingerprint collection for device identification
- [x] Real-time telemetry sparklines (Sparkline.svelte, canvas, no Chart.js)
- [x] Detailed system metrics view (SystemMetrics.svelte)
- [x] Telemetry history with time period selection (HistoryModal.svelte)
- [x] Telemetry data caching with 60s TTL
- [x] Downsampled aggregation (1m/5m/1h) for long-term history
- [x] Retention settings per owner

## FASE 19 — Installed Application Inventory
- [x] Agent collectors: apt, flatpak, pacman, AUR, AppImages
- [x] Server-side app inventory storage per device
- [x] Packages tab in DeviceModal with lazy loading
- [x] Search and filter functionality
- [x] "Show all" expand for large inventories

## FASE 20 — Documentation and Ops
- [x] README
- [x] ARCHITECTURE.md
- [x] DATA-MODEL.md
- [x] SNAPSHOT-FORMAT.md (renamed to Preference Sync Format)
- [x] AGENTS.md
- [x] COLLECTION-CONTRACT.md
- [x] ROADMAP.md
- [x] INSTALL.md
- [x] QUICKSTART.md
- [x] SECURITY.md
- [x] API.md

## FASE 21 — Reliability and Performance Hardening
- [x] Fix TEXT→time.Time scans that broke remote commands and file dedup
- [x] Unique files index `(device_id, category, relative_path, filename)` + legacy dedupe
- [x] Lightweight `/api/devices` summary projection + gzip on JSON API responses
- [x] `GET /api/devices/{id}/detail` for full hardware on demand
- [x] Device token no longer returned to the browser; security tab uses session auth
- [x] Agent: real power watts, throttled log sampling, state writes only on change
- [x] Dashboard: canvas sparklines replace per-card Chart.js; store-driven updates
- [x] Fix modal staleness, SSE owner scoping, and `kernel_version` field mismatch

## FASE 22 — Development and Production Environments
- [x] `make dev` / `make prod` / `make test` / `make help` with environment separation
- [x] Dev stack with containers: server hot-reload (air) + Vite HMR, data in `data-dev/`
- [x] Production stack: built image, persistent `./data`, required `LEM_SECRET_KEY`
- [x] `VITE_API_BASE` so the dashboard can target any API port/host
- [x] Session lifetime configurable via `LEM_SESSION_TTL_HOURS` (default 30 days)
- [x] Fresh databases create the `logs`, `http_access` and `metrics` tables

## Regras do produto

- salvar somente pequenos arquivos de texto e saves binários (base64)
- manter a sincronização focada em preferências e não em snapshots
- a web UI deve mostrar dispositivos online, última sincronização e telemetria ao vivo
- o agente é o único responsável por tocar o ambiente local
- o servidor nunca deve executar shell arbitrário no cliente
- nenhum dado sensível deve ser armazenado
- contas são o primeiro conceito: todo device pertence a um owner
- notificações usam provedores pluggable com credenciais criptografadas

## Stack e decisões iniciais

Server:
- Go 1.25
- SQLite (modernc.org/sqlite, WAL, schema em initSchema)
- AES-GCM para criptografia de credenciais de notificações

Agent:
- Go 1.22.5 (zero dependências externas)

Frontend:
- Svelte 5
- TypeScript 6
- Vite 8
- Chart.js 4

Container:
- Docker (multi-stage: Go + Node -> Alpine)
- Docker Compose

Não adicionar inicialmente:
- snapshots completos
- backups integrais da home
- restore de ambientes inteiros
- execução remota de shell (exceto Docker management via proxy)
- Redis, RabbitMQ e microserviços
