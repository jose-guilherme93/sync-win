# Processes tab — wire spec (v1)

Source of truth for the three lanes: agent, server, web. Do not invent fields.

## 1. Agent → server payload

Added to the existing hardware/summary payload (agent `main.go`).

```jsonc
"processes": [ ProcessRow, ... ],          // max 250, sorted by cpu_percent desc
"process_stats": ProcessStats,
"process_history": ProcessHistory | null,  // only on the slow (inventory) cycle
"processes_collected": true,               // false => dashboard shows "not collected", never an empty list
"process_io_available": true               // false => IO columns show "—", not 0
```

```ts
type ProcessRow = {
  pid: number; ppid: number;
  name: string;                 // /proc/pid/comm, trimmed, <=32
  cmd: string;                  // /proc/pid/cmdline, NULs -> spaces, truncated to 256 with "…"
  user: string; uid: number;    // /proc/pid/status Uid, resolved via /etc/passwd, falls back to "uid:<n>"
  state: 'R'|'S'|'D'|'Z'|'T'|'I';   // first letter of /proc/pid/stat
  threads: number;
  rss_bytes: number; vsz_bytes: number; swap_bytes: number;
  cpu_percent: number;          // delta over the window, machine capacity = 100 (same rule as today)
  cpu_time_seconds: number;     // utime+stime / USER_HZ
  start_time_unix: number;      // from /proc/pid/stat field 22 + boot time; MUST be exact (see §4)
  uptime_seconds: number;
  io_read_bps: number; io_write_bps: number;   // from /proc/pid/io; 0 when unreadable
  fd_count: number;             // len(/proc/pid/fd); 0 when unreadable
  oom_score: number;            // /proc/pid/oom_score; -1 when unreadable
  nice: number; prio: number;
  cgroup: string;               // first line of /proc/pid/cgroup, trimmed, <=200
  unit: string;                 // systemd unit derived from the cgroup path, or ""
  container: string;            // Docker container name from the cgroup path, or ""
  ports: {proto:'tcp'|'tcp6'|'udp'|'udp6', port:number, addr:string}[];  // cross-ref /proc/net/{tcp,tcp6,udp,udp6} inodes; [] when none
  kernel: boolean;              // true for [kworker] style threads (no cmdline, ppid 2)
};

type ProcessStats = {
  total: number; threads: number;
  running: number; sleeping: number; stopped: number; zombie: number; blocked: number; idle: number;
  load1: number; load5: number; load15: number;
  psi_cpu_some_avg10: number; psi_mem_some_avg10: number; psi_io_some_avg10: number;  // /proc/pressure/*, 0 when absent
  oom_kills_24h: number;       // best effort: kernel OOM counter delta; -1 when unknown
};

type ProcessHistory = {
  interval_seconds: 60;
  samples: { t: number; rows: { pid:number; name:string; cpu:number; rss:number; io_read_bps:number; io_write_bps:number }[] }[];  // last 60, top 20 by avg cpu
  events:  { t: number; type:'started'|'exited'|'oom_killed'|'restarted'; pid:number; name:string; exit_code?:number }[];  // last 200
};
```

## 2. Server → web endpoints

| Route | Notes |
|---|---|
| `GET /api/devices/{id}/processes` | `{ processes, stats, collected_at, io_available, signals_enabled }` |
| `GET /api/devices/{id}/processes/history?range=1h\|24h\|7d` | `{ series: [{name, points:[[unix_s, cpu, rss, io_read_bps, io_write_bps]]}], events: [...], buckets: [{t, top:[{name,cpu}]}] }`. The two IO values are absent from a server built before IO history: the web parser keeps them `null` rather than drawing a zero. |
| `POST /api/devices/{id}/processes/{pid}/signal` | body `{"signal":"TERM\|KILL\|STOP\|CONT"}` |
| `POST /api/devices/{id}/processes/{pid}/nice` | body `{"nice":-20..19}` |
| `POST /api/devices/{id}/processes/{pid}/ionice` | body `{"class":1\|2\|3,"level":0..7}` |

Mutating routes: owner auth + `SYNCWIN_ENABLE_REMOTE_MUTATIONS` (same gate as device actions) + agent policy. Every attempt (allowed or denied) is audited.

Queue command types: `process_signal`, `process_renice`, `process_ionice`. Payload is JSON as above; `path`/`name`/`source` are empty.

New tables: `process_history` (device_id, bucket, resolution, name, pid, cpu_sum, cpu_max, rss_max, io_read_bps, io_write_bps, samples; the IO columns hold the bucket's peak rate and are added by an idempotent boot migration) and `process_events` (device_id, ts, type, pid, name, exit_code), following the `network_history` pattern, cleaned by the existing retention job.

## 3. Agent command guard (the security boundary)

Fail closed. Every rule is a table test before the code exists.

1. Signals: allowlist `TERM, KILL, STOP, CONT` only. Everything else rejected.
2. pid: integer, `2 <= pid <= 4194304`. pid 1 always rejected.
3. Fresh check immediately before the syscall: re-read `/proc/<pid>/stat`; the process must still exist.
4. **pid-reuse (TOCTOU):** the command payload carries `start_time_unix` from the snapshot the user saw; if the live process's start time differs, reject with `pid reused` and send nothing.
5. Protected, rejected regardless of policy: the agent's own pid, `systemd`/`init`, `dockerd`, `containerd`, `containerd-shim`, `systemd-journald`, `sshd`, `sync-win-agent`, kernel threads (`kernel == true`), zombies (`Z`), and any pid whose `unit` is in the contract's protected list.
6. Policy: `/etc/sync-win/policy.json` `allow_process_signals`, default **false**. Missing key = denied.
7. Rate limit: 10 signals, renice and ionice commands per device per minute (each command type has its own 10/minute sliding window); over the limit the command fails with `rate limited`, it does not queue.
8. Never build a shell string: `syscall.Kill` only. No `exec.Command("kill", ...)`, no `sh -c`.
9. `nice` in `[-20,19]`, `ionice` class in `{1,2,3}` and level in `[0,7]`; rejected values never reach the kernel.
10. Command payload size ≤ 4 KB, strict JSON decode (unknown fields rejected).

## 4. Web rules

- Layout: **variant A** (dense full-width table + detail drawer) as the default; a `Tree` toggle switches to a ppid tree with the same inspector. Both share one renderer.
- The list owns the viewport. Toolbar, summary and pane live in one card: the toolbar is a single row of chips and selects, the summary is a one-line strip (≤ 56 px, label + value per item, sub-detail only when it is a number: shown rows, blocked, load, mem pressure), and the table/tree pane takes the remaining height with its own scroll and a sticky header. Rows default to the compact density (27 px); `Comfy` stays available.
- Search is a corner icon that opens an anchored popover (`Ctrl/⌘ K`, `Esc` closes, `Enter` keeps). An active query leaves a dot on the icon and a clear control in the popover; there is no full-width search row.
- Copy rule: **one word or one short phrase that adds real information. No explanatory paragraphs anywhere** — empty states, tooltips, footnotes, toasts. If a string does not help a decision, delete it.
- Absent ≠ broken: no IO context renders `—`, not `0`; an unknown OOM count renders `—`, not `-1`; a young process renders no chart gap as zero; `processes_collected: false` renders the honest "not collected" state naming the agent version.
- Destructive actions are `--crit`, gated, and require typing the pid for SIGKILL. Disabled controls stay visible with a one-line reason.

## 5. Tests first (red before green)

- agent: `/proc` parsing from a temp fixture dir (the collector must take a proc root argument), cpu delta math, state/ppid/cmdline truncation, io/fd/oom graceful absence, cgroup→unit/container mapping, port cross-ref; guard table for §3 (every rule), rate limit, start-time mismatch.
- server: command type registration, mutation gate off → 409, payload validation, audit rows, history retention cleanup, endpoint auth (no token / other device's token).
- web: table sort/filter/expand, drawer, gate rendering (enabled/disabled), kill modal confirm gate, history range switch.

## 6. Attack suite (run after the code exists)

Fuzz/inject: unknown fields, oversized payload, wrong types, negative/huge pid, signal not in allowlist, policy off, gate off, protected pid, pid reuse, rate-limit overflow, cross-device command, missing token, CSRF, log/name injection via `name`, `nice` -100, `ionice` class 99. Every case must be rejected with a clear reason and audited where applicable.
