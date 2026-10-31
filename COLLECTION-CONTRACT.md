# Collection Contract

Este documento é o contrato vinculante entre **agent (client)** e **server**. Tudo que o agent coleta e envia está definido aqui e no arquivo machine-readable `agent/internal/contract/contract.json` (embutido no binário). Nada fora deste contrato sai da máquina.

## Princípios

1. **O agent tem controle total** sobre o que executa localmente. Comandos vindos do servidor são *pull-only* (o agent pergunta), são validados localmente e podem ser recusados pela política local (`~/.config/sync-win/policy.json`) sem que o servidor tenha como forçar.
2. **Allowlist explícita**: nenhum diretório é varrido; apenas os arquivos listados abaixo (mais os extras autorizados pelo operador).
3. **Nada sensível trafega**: segredos, binários e arquivos de credencial são detectados e recusados antes do upload.
4. **Nada se perde**: cada campo enviado está especificado aqui com tipo e limite; o estado do agent sobrevive a restarts (`~/.local/state/sync-win/agent-state.json`).

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

- Extras: caminhos absolutos, um por linha, em `~/.config/sync-win/allowed-files`.
- Exclusões: `~/.config/sync-win/excluded-files` (por caminho ou basename).
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

`cpu_usage_percent, memory_used_bytes, memory_total_bytes, disk_read_bytes, disk_write_bytes, disk_read_rate, disk_write_rate, net_rx_rate, net_tx_rate, uptime_seconds, load_average, cpu_model, cpu_clock_mhz, core_voltage_v, kernel_version, operating_system, power_watts, architecture, desktop_environment, locale, timezone, agent_version`

`cpu_clock_mhz` é a frequência média atual entre as CPUs lógicas, lida de
`cpufreq/scaling_cur_freq` (kHz) com fallback para a linha `cpu MHz` de
`/proc/cpuinfo`; `0` significa que a fonte não existe. `core_voltage_v` só é
reportado quando um canal `hwmon` tem label de rail de CPU (`Vcore`, `CPU`,
`VDDCR`, `SVI`) e leitura na faixa plausível de 0,4–2,5 V; caso contrário é
omitido em vez de exibir um rail de 12 V como se fosse tensão de core.

Campos adicionais coletados: `battery_percent`, `battery_status`, `network_ifaces[]` (inclui `rx_errors`/`tx_errors`), `disk_partitions[]`, `swap_used_bytes`/`swap_total_bytes`, `cpu_core_usage[]`, `top_cpu_processes[]`/`top_mem_processes[]`/`stopped_processes[]`/`zombie_processes[]`/`process_summary`, `docker_available`/`docker_containers[]`, `lynis_available` e `logs[]`. `power_watts` é a potência instantânea real (watts), não a porcentagem da bateria.

### Taxas de rede são agregadas pelo agente

`net_rx_rate` e `net_tx_rate` são bytes por segundo **somados pelo agente somente sobre as interfaces contadas** (`counted=true`). O dashboard deve consumir esses campos e **nunca somar `network_ifaces[]` por conta própria**: somar a lista inteira contaria o mesmo byte no par `veth`, na sua bridge e no `docker0`.

### Interfaces de rede são reportadas, não descartadas

`/proc/net/dev` lista toda a tubulação de containers e VMs, e o mesmo byte é contabilizado no par `veth`, na sua bridge e no `docker0`. Em um host com 14 containers isso inflava o total em ordens de grandeza.

O collector agora **reporta todas as interfaces** e marca cada uma com dois campos:

- `kind`: `physical`, `wifi`, `tunnel`, `bridge`, `veth`, `loopback` ou `virtual`
- `counted`: `false` para a tubulação de containers e VMs, `true` para o resto

As interfaces com `counted=false` são `lo`, `veth*`, `br-*`, `br0`, `docker*`, `virbr*`, `vnet*`, `cni*`, `flannel*`, `cali*`, `kube-ipvs*`. Interfaces reais e túneis deliberados são mantidas e contadas: `eth0`, `wlan0`, `enp3s0`, `tailscale0`, `wg0`, `tun0`, `tap0`.

Elas continuam no payload porque o operador precisa ver a bridge que está carregando o tráfego; o que não pode acontecer é elas entrarem no total. Um agent anterior a 1.8.0 não manda o campo `counted`, e nesse caso **todas** as interfaces recebidas são contadas — ele só reportava as reais.

Além dos contadores de sempre, cada interface carrega `driver`, `duplex`, `carrier_changes`, `rx_fifo_errors`, `tx_fifo_errors`, `rx_collisions`, `tx_carrier_errors` e `multicast`. Todos são best-effort: podem vir vazios.

### Saúde de pacotes (`network_health`)

O bloco `network_health` chega na telemetria e traz o que o kernel já contava:

- `tcp`: `active_opens`, `passive_opens`, `attempt_fails`, `estab_resets`, `curr_estab`, `in_segs`, `out_segs`, `retrans_segs`, `in_errs`, `out_rsts`, `retrans_per_sec` e `retrans_percent`
- `udp`: `in_datagrams`, `no_ports`, `in_errors`, `out_datagrams`, `rcvbuf_errors`, `sndbuf_errors`
- `sockets`: contagem por estado (`established`, `time_wait`, `close_wait`, `syn_sent`, `syn_recv`, `fin_wait1`, `fin_wait2`, `listen`, `udp`)
- `conntrack`: `count` e `max` do netfilter, quando o kernel tem conntrack

`retrans_per_sec` é um **delta** contra o ciclo anterior, como toda taxa do agent. `retrans_percent` é a fração acumulada de segmentos retransmitidos desde o boot. O bloco nunca derruba o ciclo de telemetria: um arquivo de `/proc` ilegível deixa o contador em zero, e não o ciclo em erro.

### Topologia de rede (inventário de sistema)

A topologia viaja no inventário de sistema (`POST /api/devices/{id}/system-inventory`), no ritmo lento de 300 s, e não na telemetria. Ela é lida de `/proc/net/route`, `/proc/net/ipv6_route`, `/etc/resolv.conf`, `/proc/net/arp`, `/proc/net/ndisc_cache` e `/proc/net/wireless`.

O agent roda **sem privilégio**, e isso define o que existe no payload:

- **gateway padrão, rotas, resolvers e domínios de busca**: disponíveis
- **vizinhos (ARP/NDP)**: disponíveis
- **qualidade do rádio** (sinal, ruído, quality): disponível; **SSID, canal e bitrate não**, porque precisam da API nl80211
- **regras de firewall**: inacessíveis. O payload traz só o **estado** do serviço (`ufw`, `firewalld`, `nftables` como `active`, `inactive`, `failed` ou `not installed`), derivado das units systemd já coletadas
- **peers de Tailscale**: disponíveis pelo socket local `/var/run/tailscale/tailscaled.sock`, que é legível por qualquer usuário na instalação padrão (0666). O agent faz um `GET` em `/localapi/v0/status` via HTTP sobre Unix socket; nunca executa o binário `tailscale` e nunca lê chaves
- **peers de WireGuard e de outros daemons**: inacessíveis sem root. O que se vê é a interface do túnel e seus contadores; a tela de topologia nomeia esses túneis em vez de omiti-los

A seção `overlays` dentro de `topology` descreve cada rede privada que o daemon expõe:

- `{kind, name?, magic_dns?, interface?, state, self?, peers[], error?}`
- `self` e cada peer: `{hostname, dns_name?, os?, ips[], online, direct?, relay?, exit_node?, exit_node_option?, tags?, last_seen?}`
- `state` é o estado do backend: `running`, `stopped`, `needs login` ou `unknown`
- `error` explica por que a lista de peers não veio. Um socket ilegível e uma rede sem peers são casos diferentes e não podem parecer iguais

Os peers são ordenados com os online primeiro e cortados em `max_overlay_peers`; um tailnet grande nunca perde os nós alcançáveis para o limite. Um daemon ausente não gera overlay nenhum. Os limites (`max_routes`, `max_neighbors`, `max_wireless`, `max_tunnels`, `max_overlay_networks`, `max_overlay_peers`) vivem em `system_inventory.network` no `contract.json`.

A topologia de LAN é passiva: a tabela ARP/NDP só contém dispositivos com os quais esta máquina trocou tráfego. O agent não faz ping sweep nem varredura de portas; a tela diz isso explicitamente em vez de apresentar o vizinho como a rede inteira.

### Process snapshot: top consumers e estados de problema

O payload carrega quatro listas e um resumo do estado de **todos** os processos:

| Campo | Limite | Conteúdo |
|---|---|---|
| `top_cpu_processes[]` | 20 | maiores consumidores de CPU na janela |
| `top_mem_processes[]` | 20 | maiores RSS (a mesma CPU normalizada) |
| `stopped_processes[]` | 10 | estado `T`/`t`, mais antigos primeiro |
| `zombie_processes[]` | 10 | estado `Z`, mais antigos primeiro |
| `process_summary` | — | `total`, `running`, `sleeping`, `disk_sleep`, `stopped`, `zombie`, `other`, `threads` de todos os processos |

Cada linha reportada carrega `pid`, `ppid`, `name`, `user`, `state`,
`cpu_percent`, `mem_rss_bytes`, `mem_virt_bytes`, `threads`,
`elapsed_seconds` e `command` (truncado em 200 caracteres). Os caps vivem em
`processes` no `contract.json`.

Custo fixo: **uma leitura de `/proc/<pid>/stat` por processo** — o `comm`, o
RSS e o estado vêm do mesmo arquivo. Só as linhas reportadas pagam por
`/proc/<pid>/status` (uid) e `/proc/<pid>/cmdline`, e `/etc/passwd` é lido uma
vez por ciclo. É mais barato que a coleta anterior, que lia `comm`, `status` e
`stat` de todo processo, e ainda entrega muito mais contexto.

**CPU é delta e é participação na máquina**: `cpu_percent` é a porcentagem
consumida na janela entre dois ciclos, delta de jiffies (`utime + stime`)
dividido pelo tempo decorrido e pelo número de CPUs lógicas, limitada a 0–100.
Um core saturado em uma máquina de quatro cores lê 25%; `threads` é reportado
separadamente para que um processo multithread não pareça pequeno. O primeiro
ciclo e PIDs novos reportam `0` porque não há delta. O total em
`process_summary` cobre a máquina inteira, então a UI nunca deve apresentar as
listas como se fossem todos os processos.

### Logs de journal (`logs[]`)

`logs[]` chega em `POST /api/devices/{id}/telemetry` com os campos `timestamp`,
`level`, `source` e `message`. O agent **não** coleta a cada ciclo: amostra o
journal a cada 6 ciclos (cerca de 60 s no intervalo padrão de 10 s) porque
`journalctl` é a coleta mais pesada do ciclo.

> **Requisito de permissão.** O agente roda como `sync-win` e **precisa** ser
> membro de `systemd-journal` (ou `adm`) para ler o journal. Os arquivos do
> journal são `0640 root:systemd-journal`, então um agente fora desse grupo leva
> `Permission denied` do `journalctl` e a tela de Logs fica vazia em todos os
> dispositivos, sem erro visível. A unit já declara
> `SupplementaryGroups=docker systemd-journal`; o script de instalação escreve a
> mesma linha. Em caso de tela vazia, confira
> `grep Groups /proc/$(pidof sync-win-agent)/status` — se `systemd-journal` não
> estiver listado, a unit não foi atualizada.

O agent também distingue **journal vazio** de **journal ilegível**: um
`journalctl` que sai com status 0 e nenhuma saída gera erro explícito, porque
`--quiet` com journal inacessível pode terminar em 0 sem ter lido nada.

O server **persiste** essas linhas em `device_logs`. Isso é o que permite a tela
mostrar histórico: `hardware_json` é sobrescrito inteiro a cada post de
telemetria, então um lote só sobrevive até o próximo ciclo que não traz logs.

- Ingestão é idempotente: `(device_id, ts, source, message)` é único, então as
  janelas sobrepostas que o agent reenvia são gravadas uma única vez.
- Linha sem timestamp parseável é descartada, não gravada como inconsultável.
- `message` passa pela redação de segredos e é truncada em 2000 bytes.
- `timestamp` é o formato `short-iso` do journalctl (`2026-10-07T13:03:15-0300`).
  O offset não tem dois-pontos, então `time.Parse(time.RFC3339)` o rejeita; o
  server aceita o layout específico. Consumidores devem aceitar o mesmo.
- `source` nunca mantém os dois-pontos finais. `journalctl` escreve `kernel:`
  para fontes sem pid, e manter o `:` dividia um serviço em dois buckets no
  filtro de origem do dashboard.
- Severidade vem de palavras-chave na mensagem (`ERROR`/`FAIL`/`CRIT`,
  `WARN`), não da prioridade do journald: `journalctl` é chamado no formato
  `short-iso`, que não carrega prioridade. O server normaliza para `error`,
  `warn` ou `info`, e qualquer valor não reconhecido vira `info`.
- **Falha de leitura é reportada, não silenciosa.** O agent roda como usuário de
  serviço sem privilégio, então um journal ilegível é um resultado real de
  implantação (sem journald, sem permissão, host sem systemd). `CollectDeviceLogs`
  retorna o erro — incluindo o **stderr** do `journalctl`, que é onde aparece a
  causa acionável — e o agent o registra no próprio journal, que é justamente o
  journal que a tela tenta ler.

### Partições de disco são reportadas uma vez por device

Em ext4 o sistema reporta `/home`, `/root`, `/srv` e outros diretórios como mounts separados do mesmo device, o que listava o mesmo disco várias vezes. O collector deduplica por device, mantendo o mount mais raso. Subvolumes **btrfs** são a exceção e permanecem, pois cada um é um mount independente com uso próprio.

Fontes: `/proc/stat`, `/proc/meminfo`, `/proc/diskstats`, `/proc/uptime`, `/proc/loadavg`, `/proc/cpuinfo`, `/proc/net/dev`, `/proc/mounts` + `statfs`, `/proc/<pid>/stat`, `/proc/<pid>/status`, `/sys/class/power_supply/*/power_now`, `/etc/os-release`, `$XDG_CURRENT_DESKTOP`, `$LANG`, `/etc/timezone`. `logs[]` é amostrado a cada 6 ciclos (~60 s) via `journalctl`. Intervalo padrão: 10 s.

## Inventário de apps (`POST /api/devices/{id}/apps`)

Fontes: `apt-mark showmanual`, `flatpak --user list`, `pacman -Qqe` (menos `-Qmq`), `paru/yay -Qmq`, AppImages em `~/Applications` e `~/.local/bin`. Campos: `{name, version?, source, path?}` com `source ∈ {apt, flatpak, pacman, aur, appimage}`. Atualiza a cada 5 min junto do ciclo de preferências.

## Inventário de sistema (`POST /api/devices/{id}/system-inventory`)

Quatro seções independentes, declaradas em `system_inventory` no `contract.json`:
unidades systemd, sockets em escuta, contas de login e topologia de rede. Intervalo
padrão 300 s.

Fontes: `systemctl list-units --type=service --all` +
`systemctl list-unit-files --type=service` para as unidades, `ss -tulpnH` para os
sockets. Cada comando é limitado por timeout (`services.timeout_seconds`,
`ports.timeout_seconds`) e a saída é cortada em `max_units` / `max_ports`.

### Unidades systemd

Campos: `{name, status, load_state, active_state, sub_state?, unit_file_state?, description?, enabled}`.
`status` é o balde que o dashboard colore (`running`, `failed`, `stopped`), não o
valor cru do systemd — um serviço saudável reporta `ACTIVE=active` e
`SUB=running`, e a coluna `ACTIVE` sozinha não diz nada a quem lê. As colunas
crus seguem no payload. Só entram unidades cujo `status` esteja em `states`.

`enabled` é verdadeiro quando o unit file state está em `enabled_states`. A
listagem de unit files é uma segunda chamada: se ela falhar, o snapshot sobe sem
a flag, e não é perdido.

As unidades são ordenadas por `failed`, `running`, `stopped` e depois por nome, de
modo que uma unidade quebrada não fique enterrada entre centenas de unidades
paradas.

### Sockets em escuta

Campos: `{protocol, local_address, port, process?, pid?}`. Só o nome do processo e
o pid são reportados — nunca a linha de comando, que poderia conter segredo. O
`ss` traz uma coluna `state` que alguns builds omitem em sockets de datagrama, e
endereços IPv6 vêm entre colchetes, às vezes com zone ID (`[::%eth0]:22`,
`127.0.0.53%lo:53`). O parser trata os dois casos e normaliza o wildcard para
endereço vazio.

### Contas de login (remote access)

Campos: `{enabled, default_user?, logins[]}`. `enabled` reflete a política local
(`allow_remote_access`) e `default_user` o alvo configurado (`ssh_user`), de modo
que o dashboard sabe o estado do device sem poder alterá-lo. `logins` lista as
contas que uma sessão remota pode usar: lidas de `/etc/passwd`, apenas uid ≥
`users.min_uid` (padrão 1000), com shell de login real; `nologin`/`false`, contas
de sistema e `root` são excluídas. Limitado a `users.max_users`. Nenhum dado de
usuário além do nome é coletado. A lista é enviada mesmo com o acesso desligado,
para o operador ver as opções antes de habilitar.

### Ferramenta ausente não é falha

`systemctl` ou `ss` ausentes retornam lista vazia sem erro: o agent roda também em
sistemas sem systemd e um binário faltando não pode virar erro de ciclo a cada
5 minutos. Já um comando presente que falha é erro — a seção correspondente é
omitida do payload e o server preserva o snapshot anterior dela.

O payload aceita `services`, `ports`, `users` e `network` ausentes. Uma seção
omitida preserva o último valor; um array vazio é um relatório real e substitui o
snapshot.

## Heartbeat (`POST /api/devices/{id}/heartbeat`)

Corpo: `{"device_token": "..."}` — zera contador de falhas e marca online. O server deriva status: online (<30 s), stale (>30 s), offline (>5 min), error (último erro mais novo que último seen).

## Atualizações pendentes

O agente consulta atualizações disponíveis apenas com comandos de leitura:
`apt list --upgradable`, `flatpak remote-ls --updates`, `pacman -Qu` e
`paru/yay -Qua`. Nenhum desses comandos instala ou baixa algo. O resultado é
enviado em `POST /api/devices/{id}/updates` com um estado explícito:

| Estado | Significado |
|---|---|
| `ready` | A consulta rodou; `updates` lista o que está pendente (pode ser vazio) |
| `unsupported` | Nenhum gerenciador suportado está instalado |
| `error` | Um gerenciador instalado falhou ou excedeu tempo/saída |

O servidor rejeita fontes, nomes e versões fora do contrato e nunca guarda
`updates` quando o estado não é `ready`, para que uma falha não seja lida como
"sem atualizações". A tela `Packages` mostra os quatro estados.

## Configuração do dispositivo

O dashboard atualiza nome de exibição, tags e intervalo de telemetria em
`PATCH /api/devices/{id}` (somente sessão do proprietário). O agente consulta
`GET /api/devices/{id}/settings` com o token do dispositivo após enviar
telemetria; o intervalo retornado passa a valer para o próximo ciclo. São aceitos
5, 10, 30 ou 60 segundos. Se a consulta falhar ou retornar valor inválido, o
agente mantém o intervalo atual. Os intervalos de sync de preferências, saves e
inventário continuam independentes.

## Comandos (pull via `GET /api/devices/{id}/commands`, resultado via `POST .../commands/{cmd}`)

| Tipo | Ação local | Validações do agent |
|---|---|---|
| `install_app` | apt-get/flatpak/pacman/paru/yay install | nome validado (charset restrito), fonte conhecida, **timeout** configurável (padrão 900 s), saída limitada a 64 KiB, executado apenas com `allow_install_app=true` na policy |
| `exclude_file` | adiciona linha em excluded-files | caminho relativo, sem `..`; idempotente |
| `lynis_audit` | executa `lynis audit system --cronjob --no-colors` | Lynis deve estar instalado; timeout 600 s; parseia `lynis-report.dat` em JSON estruturado (hardening_index, warnings, suggestions, categories); resultado armazenado no servidor em `security_audits` |
| `restart_agent` | encerra o daemon após reportar o resultado; systemd reinicia o serviço | requer `allow_restart_agent=true` e serviço systemd (Restart=always) |
| `update_packages` | atualiza APT, Flatpak, Pacman ou AUR pelos executáveis fixos | requer `allow_package_updates=true`; timeout local configurado; APT/Pacman/system Flatpak exigem root ou `sudo -n` |
| `reboot_device` | agenda `shutdown -r +1` | requer `allow_reboot_device=true` e root ou `sudo -n`; resultado enviado antes do reboot |

`install_app` usa apenas comandos fixos por fonte. Flatpak e AUR rodam no user service; APT e Pacman exigem root ou `sudo -n` configurado. AppImage é recusado porque não há fonte.download confiável. A feature flag `SYNCWIN_ENABLE_REMOTE_MUTATIONS` também precisa estar habilitada no servidor.

- **Policy local** (`/etc/sync-win/policy.json`, criada pelo operador; o caminho legado `~/.config/sync-win/policy.json` ainda é lido): `{"allow_install_app": false, "allow_exclude_file": false, "allow_restore_saves": false, "allow_lynis_audit": true, "allow_restart_agent": false, "allow_package_updates": false, "allow_reboot_device": false, "allow_docker_read": true, "allow_docker_lifecycle": false, "allow_docker_exec": false, "allow_docker_prune": false, "allow_docker_compose": false, "allow_docker_images": false, "allow_remote_access": false, "ssh_user": "<usuário>", "command_timeout_seconds": 900}`. Arquivo ausente ou inválido usa defaults fail-closed. Comando recusado é reportado ao servidor com motivo — nada executa sem consentimento local. Alterada no device por `sync-win-agent set <ssh|ssh-user>`, que reescreve o arquivo por cima dos defaults (nunca zera as outras capabilities) de forma atômica em modo `0600`.
- **Docker**: IDs de container/exec são validados, requests têm deadline, respostas são limitadas e compose aceita apenas filenames/roots aprovados sem symlink escape. Results são vinculados ao device que os solicitou. Imagens: `image_list` e `image_inspect` são leitura (`allow_docker_read`); `image_pull` e `image_remove` exigem `allow_docker_images`, aceitam apenas referência válida (nome, tag opcional, digest sha256) e o pull tem deadline próprio (`docker_pull_timeout_seconds`, 600 s) porque baixar camadas não é uma ação de lifecycle.
- Transporte: token via header `Authorization: Bearer`; polling apenas; o servidor nunca empurra nada.

## Resiliência

- HTTP timeout: 15 s por requisição.
- Backoff exponencial em falhas consecutivas de ciclo: intervalo × 2^n até 10 min, com jitter de ~10% (evita sincronização de rebanho).
- Estado persistente: hashes separados para preferências e saves + timestamps de sincronização; restart não re-envia arquivo inalterado nem perde o agendamento.

  Onde esse estado vive depende de como o agente foi iniciado. Como serviço de
  sistema, a unit declara `StateDirectory=sync-win`, e o systemd então cria
  `/var/lib/sync-win` com o dono do usuário do serviço, define
  `STATE_DIRECTORY=/var/lib/sync-win` e o inclui no conjunto gravável — o agente
  o usa. Isso é o que faz o estado sobreviver: o caminho XDG resolve sob um
  `HOME` que o usuário de serviço não tem (o instalador cria com
  `--no-create-home`), dentro de um filesystem que `ProtectSystem=strict` monta
  como somente-leitura.

  Sem `STATE_DIRECTORY` — execução manual, ou o modo legado de user-systemd — o
  agente usa `$XDG_STATE_HOME/sync-win/agent-state.json` e, na ausência dele,
  `~/.local/state/sync-win/agent-state.json`.
- systemd user unit gerada pelo `install.sh`: `Restart=always`, `RestartSec=15`, `StartLimitIntervalSec=0` (nunca entra em ban).
- Arquivos individuais problemáticos são pulados com log — um arquivo ruim nunca aborta o lote.

## Auto-atualização do agente

O daemon roda como usuário de serviço sem privilégio: não pode substituir
`/usr/local/bin/sync-win-agent` nem escrever em `/etc/systemd/system`. Ele
**pede** a atualização, e uma unit de caminho com privilégio a executa.

- O daemon escreve `/var/lib/sync-win/update-request` (diretório dele, dentro de
  `ReadWritePaths`) quando o servidor anuncia versão mais nova.
- `sync-win-agent-update.path` observa esse arquivo e dispara
  `sync-win-agent-update.service`, que roda como root.
- O serviço reconcilia **binário e unit** a cada execução. Uma unit pode estar
  desatualizada mesmo com o binário em dia — foi assim que uma frota ficou sem
  acesso ao journal parecendo saudável.
- Credenciais para buscar a unit autenticada vêm da própria unit
  (`--device-id`/`--device-token`), não do arquivo de estado: o serviço de update
  roda como root, onde o path de estado resolve sob outro `HOME`.

Se o agente não consegue ler o journal, ele solicita um **reparo de unit**, no
máximo uma vez por hora, porque a correção exige root.

## Versionamento do contrato

`contract_version` no JSON segue SemVer. Mudanças incompatíveis de schema exigem bump major e atualização simultânea deste arquivo. Testes automatizados garantem que constantes de runtime e o JSON embutido não divergem.
