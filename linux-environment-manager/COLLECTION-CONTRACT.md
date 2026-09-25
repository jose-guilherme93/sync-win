# Collection Contract

Este documento é o contrato vinculante entre **agent (client)** e **server**. Tudo que o agent coleta e envia está definido aqui e no arquivo machine-readable `agent/internal/contract/contract.json` (embutido no binário). Nada fora deste contrato sai da máquina.

## Princípios

1. **O agent tem controle total** sobre o que executa localmente. Comandos vindos do servidor são *pull-only* (o agent pergunta), são validados localmente e podem ser recusados pela política local (`~/.config/lem/policy.json`) sem que o servidor tenha como forçar.
2. **Allowlist explícita**: nenhum diretório é varrido; apenas os arquivos listados abaixo (mais os extras autorizados pelo operador).
3. **Nada sensível trafega**: segredos, binários e arquivos de credencial são detectados e recusados antes do upload.
4. **Nada se perde**: cada campo enviado está especificado aqui com tipo e limite; o estado do agent sobrevive a restarts (`~/.local/state/lem/agent-state.json`).

## Arquivos de preferência coletados

| Caminho padrão | Categoria |
|---|---|
| `~/.bashrc` | shell |
| `~/.profile` | shell |
| `~/.zshrc` | shell |
| `~/.config/kdeglobals` | kde |
| `~/.config/gtk-3.0/settings.ini` | desktop |
| `~/.config/gtk-4.0/settings.ini` | desktop |
| `~/.config/Code/User/settings.json` | app |

- Extras: caminhos absolutos, um por linha, em `~/.config/lem/allowed-files`.
- Exclusões: `~/.config/lem/excluded-files` (por caminho ou basename).
- Limite por arquivo: **512 KiB** (agente) / espelhado no servidor: **256 KiB**, request total: **2 MiB**.
- Categorias válidas: `kde`, `desktop`, `shell`, `app`, `general`, `workspace`, `saves`.
- Recusas automáticas: conteúdo vazio, binário/Não-UTF8 (exceto categoria `saves` com `encoding: "base64"`), padrões de segredo (PEM private key, AWS AKIA, GitHub `ghp_`/`gho_`/etc., Slack `xox*-`, Stripe `sk_live_`, Google `AIza…`) e filenames sensíveis (`id_rsa*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`). Para `encoding: "base64"` a verificação de segredos é pulada.

### Workspace projects (categoria `workspace`)

- Raízes: pastas de projeto configuradas pelo operador em `workspace_dirs` (`GET/PUT /api/sync-config`).
- Arquivos explícitos: `.vscode/settings.json`, `.vscode/tasks.json` e `.vscode/launch.json`.
- A pasta raiz não é varrida: o agent verifica apenas esses caminhos relativos.
- Limites: 256 KiB por arquivo e 2 MiB por ciclo.
- Symlinks, binários, conteúdo secret ou paths fora de `$HOME` são ignorados.
- O agent mantém hashes separados dos demais preferences.

### Save Games (categoria `saves`)

| Raiz padrão | Filtro |
|---|---|
| `~/.config/hydralauncher/wine-prefixes/*/drive_c/users/*/Documents` | ext allowlist + profundidade ≤12, exclui `Temp`, `Cache*`, `content` etc. |
| `~/.config/hydralauncher/wine-prefixes/*/drive_c/users/*/Saved Games` | mesmo filtro |
| `~/.config/hydralauncher/wine-prefixes/*/drive_c/users/*/AppData/Roaming` | apenas ext allowlist |
| `~/.config/hydralauncher/ludusavi/config.yaml` | arquivo único |
| `~/.steam/steam/userdata` | mesmo filtro |
| `~/.var/app/com.valvesoftware.Steam/.steam/steam/userdata` | mesmo filtro |
| `~/.config/unity3d` | mesmo filtro |

- Extensões permitidas: `.save`, `.sav`, `.sgm`, `.srm`, `.mcr`, `.mcd`, `.savemeta`, `.bin`, `.dat`, `.cfg`, `.ini`, `.json`, `.yaml`, `.yml`, `.xml`, `.txt`, `.vdf`, `.prof`, `.litematic`
- Diretórios excluídos (em qualquer nível): `Temp`, `temp`, `Cache`, `Code Cache`, `GPUCache`, `DawnGraphiteCache`, `DawnWebGPUCache`, `INetCache`, `INetCookies`, `History`, `Crashpad`, `blob_storage`, `Shared Dictionary`, `content`, `Content`, `CommonRedist`, `redist`, `__installer`, `shadercache`
- Extensões lixo: `.tmp`, `.log`, `.dmp`, `.bak`, `.old`, `.lock`
- Limites: **1 MiB** por arquivo, **16 MiB** total por ciclo de sync
- Arquivos binários são enviados como base64 (`encoding: "base64"`); texto permanece como UTF-8
- Extras do operador: configurados via dashboard (GET/PUT `/api/sync-config`) e servidos ao agent via `GET /api/devices/{id}/sync-config`

### Payload de sync (`POST /api/devices/{id}/sync`)

```json
{
  "device_token": "...",
  "preferences": [
    {
      "category": "shell",
      "filename": "bashrc",
      "relative_path": ".bashrc",
      "content": "<texto utf-8>"
    },
    {
      "category": "saves",
      "filename": "Slot_00000002.save",
      "relative_path": ".config/hydralauncher/wine-prefixes/1222670/drive_c/users/steamuser/Documents/Electronic Arts/The Sims 4/saves/Slot_00000002.save",
      "content": "<base64>",
      "encoding": "base64"
    }
  ]
}
```

Resposta: `{"saved": [PreferenceFile...], "rejected": [{"filename", "reason"}]}` — o servidor deduplica por hash de conteúdo.

O agent sincroniza saves no mesmo intervalo padrão de preferências (300 s), em lotes menores que o limite HTTP do servidor. O estado mantém hashes separados para preferências e saves; um hash só avança após o servidor confirmar o item como salvo.

### Restore de saves

O comando `restore_saves` é pulling, fica sujeito à feature flag de mutações remotas e à política local `allow_restore_saves` (por padrão `false`). O servidor envia no payload:

```json
{
  "source_device_id": "...",
  "prefix_id": "1222670",
  "game_name": "Publisher/Game",
  "files": [
    {
      "filename": "slot.sav",
      "relative_path": ".config/hydralauncher/wine-prefixes/1222670/drive_c/users/user/Documents/Game/slot.sav",
      "content": "...",
      "encoding": "base64"
    }
  ]
}
```

O agent valida tamanho, encoding e paths relativos, rejeita traversal/symlinks, grava cada arquivo por arquivo temporário + rename e processa somente arquivos de saves do prefixo/jogo solicitados. A feature flag do servidor deve estar habilitada e a política local do dispositivo deve permitir a operação.

## Telemetria (`POST /api/devices/{id}/telemetry`)

Campos enviados (todos opcionais no display, mas parte do contrato):

`cpu_usage_percent, memory_used_bytes, memory_total_bytes, disk_read_bytes, disk_write_bytes, disk_read_rate, disk_write_rate, uptime_seconds, load_average, cpu_model, kernel_version, operating_system, power_watts, architecture, desktop_environment, locale, timezone, agent_version`

Campos adicionais coletados: `battery_percent`, `battery_status`, `network_ifaces[]` (inclui `rx_errors`/`tx_errors`), `disk_partitions[]`, `swap_used_bytes`/`swap_total_bytes`, `cpu_core_usage[]`, `top_cpu_processes[]`/`top_mem_processes[]`, `docker_available`/`docker_containers[]`, `lynis_available` e `logs[]`. `power_watts` é a potência instantânea real (watts), não a porcentagem da bateria.

Fontes: `/proc/stat`, `/proc/meminfo`, `/proc/diskstats`, `/proc/uptime`, `/proc/loadavg`, `/proc/cpuinfo`, `/proc/net/dev`, `/sys/class/power_supply/*/power_now`, `/etc/os-release`, `$XDG_CURRENT_DESKTOP`, `$LANG`, `/etc/timezone`. `logs[]` é amostrado a cada 6 ciclos (~60 s) via `journalctl`. Intervalo padrão: 10 s.

## Inventário de apps (`POST /api/devices/{id}/apps`)

Fontes: `apt-mark showmanual`, `flatpak --user list`, `pacman -Qqe` (menos `-Qmq`), `paru/yay -Qmq`, AppImages em `~/Applications` e `~/.local/bin`. Campos: `{name, version?, source, path?}` com `source ∈ {apt, flatpak, pacman, aur, appimage}`. Atualiza a cada 5 min junto do ciclo de preferências.

## Heartbeat (`POST /api/devices/{id}/heartbeat`)

Corpo: `{"device_token": "..."}` — zera contador de falhas e marca online. O server deriva status: online (<30 s), stale (>30 s), offline (>5 min), error (último erro mais novo que último seen).

## Comandos (pull via `GET /api/devices/{id}/commands`, resultado via `POST .../commands/{cmd}`)

| Tipo | Ação local | Validações do agent |
|---|---|---|
| `install_app` | apt-get/flatpak/pacman/paru/yay install | nome validado (charset restrito), fonte conhecida, **timeout** configurável (padrão 900 s), saída limitada a 64 KiB, executado apenas com `allow_install_app=true` na policy |
| `exclude_file` | adiciona linha em excluded-files | caminho relativo, sem `..`; idempotente |
| `lynis_audit` | executa `lynis audit system --cronjob --no-colors` | Lynis deve estar instalado; timeout 120 s; parseia `lynis-report.dat` em JSON estruturado (hardening_index, warnings, suggestions, categories); resultado armazenado no servidor em `security_audits` |

`install_app` usa apenas comandos fixos por fonte. Flatpak e AUR rodam no user service; APT e Pacman exigem root ou `sudo -n` configurado. AppImage é recusado porque não há fonte.download confiável. A feature flag `LEM_ENABLE_REMOTE_MUTATIONS` também precisa estar habilitada no servidor.

- **Policy local** (`~/.config/lem/policy.json`, criada pelo operador): `{"allow_install_app": false, "allow_exclude_file": false, "allow_restore_saves": false, "allow_lynis_audit": true, "allow_docker_read": true, "allow_docker_lifecycle": false, "allow_docker_exec": false, "allow_docker_prune": false, "allow_docker_compose": false, "command_timeout_seconds": 900}`. Arquivo ausente ou inválido usa esses defaults fail-closed. Comando recusado é reportado ao servidor com motivo — nada executa sem consentimento local.
- **Docker**: IDs de container/exec são validados, requests têm deadline, respostas são limitadas e compose aceita apenas filenames/roots aprovados sem symlink escape. Results são vinculados ao device que os solicitou.
- Transporte: token via header `Authorization: Bearer`; polling apenas; o servidor nunca empurra nada.

## Resiliência

- HTTP timeout: 15 s por requisição.
- Backoff exponencial em falhas consecutivas de ciclo: intervalo × 2^n até 10 min, com jitter de ~10% (evita sincronização de rebanho).
- Estado persistente: hashes separados para preferências e saves + timestamps de sincronização; restart não re-envia arquivo inalterado nem perde o agendamento.
- systemd user unit gerada pelo `install.sh`: `Restart=always`, `RestartSec=15`, `StartLimitIntervalSec=0` (nunca entra em ban).
- Arquivos individuais problemáticos são pulados com log — um arquivo ruim nunca aborta o lote.

## Versionamento do contrato

`contract_version` no JSON segue SemVer. Mudanças incompatíveis de schema exigem bump major e atualização simultânea deste arquivo. Testes automatizados garantem que constantes de runtime e o JSON embutido não divergem.
