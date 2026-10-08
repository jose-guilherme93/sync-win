#!/usr/bin/env bash
# End-to-end smoke test: seeds a device through the real API and verifies the
# dashboard renders it in a real browser.
#
# Why this exists separately from `make test`: unit tests, `vite build` and
# `svelte-check` all passed while two layout faults and one navigation fault
# were live in the UI. Static verification cannot see layout, focus, or the
# assembled request/response path, so the dashboard claims are checked here.
#
# What it covers:
#   - registration, enrollment, and the agent upload endpoint
#   - device selection from the fleet table enabling the scoped sidebar
#   - the Services screen rendering seeded services and ports
#   - both filters, "problems only", and the error state with retry
#
# What it does not cover: the nine other screens, responsive breakpoints, and
# anything requiring a real agent on a real machine.
#
# Usage:
#   scripts/e2e.sh              # assumes `make dev-d` is already running
#   scripts/e2e.sh --fresh      # wipe data-dev first so the seed is clean
#
# Requires: curl, python3, jq-free JSON via python3, and agent-browser.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_PORT="${WEB_PORT:-5173}"
API_PORT="${DEV_HTTP_PORT:-8088}"
BASE="http://localhost:${WEB_PORT}"
API="http://localhost:${API_PORT}"

EMAIL="e2e-$$@example.com"
PASSWORD="supersecret1"
HOSTNAME="e2e-laptop-$$"

WORK="$(mktemp -d)"
COOKIES="${WORK}/cookies.txt"
FAILURES=0
CHECKS=0

# agent-browser is not reliably on PATH: global npm installs land in the
# versioned node prefix, and `npm bin -g` no longer exists on npm 9+. Derive the
# prefix instead and probe the handful of places a global install can end up.
if ! command -v agent-browser >/dev/null 2>&1; then
  candidates=()
  npm_prefix="$(npm prefix -g 2>/dev/null || true)"
  [ -n "${npm_prefix}" ] && candidates+=("${npm_prefix}/bin/agent-browser")
  npm_root="$(npm root -g 2>/dev/null || true)"
  [ -n "${npm_root}" ] && candidates+=("${npm_root}/../../bin/agent-browser")
  candidates+=(
    "${HOME}/.local/bin/agent-browser"
    "${HOME}/.npm-global/bin/agent-browser"
    "/usr/local/bin/agent-browser"
  )
  for candidate in "${candidates[@]}"; do
    if [ -x "${candidate}" ]; then
      PATH="$(dirname "${candidate}"):${PATH}"
      break
    fi
  done
fi

if ! command -v agent-browser >/dev/null 2>&1; then
  echo "agent-browser not found. Install it with:" >&2
  echo "  npm i -g agent-browser && agent-browser install" >&2
  exit 2
fi

cleanup() {
  timeout "${AB_CMD_TIMEOUT:-45}" agent-browser close --all >/dev/null 2>&1 || true
  rm -rf "${WORK}"
}
trap cleanup EXIT

ok()   { CHECKS=$((CHECKS+1)); printf '  ok   %s\n' "$1"; }
fail() { CHECKS=$((CHECKS+1)); FAILURES=$((FAILURES+1)); printf '  FAIL %s\n     %s\n' "$1" "${2:-}"; }

# check <label> <actual> <expected-substring>
check() {
  if [[ "${2}" == *"${3}"* ]]; then ok "$1"; else fail "$1" "expected to contain '${3}', got '${2}'"; fi
}

section() { printf '\n== %s\n' "$1"; }

json_get() { python3 -c "import json,sys;d=json.load(sys.stdin);print(d${1})"; }

# eval_js <js> — run in the page and print the result. Agent-browser's own click
# and fill are unreliable here because element refs go stale on every re-render,
# which silently clicked the wrong control during manual testing. Selecting the
# target by its own text inside the page removes that whole failure mode.
# eval_js runs the expression in the page and prints the last line. agent-browser
# prints the return value as a JSON string, so plain values come back quoted
# ("CLICKED"). unquote strips that so shell comparisons stay readable.
#
# Every agent-browser call is wrapped in `timeout`. Without it a wedged CDP
# session hangs the whole suite and burns the job's full timeout: one run sat at
# 19 minutes instead of 4. A bound here turns a hang into a normal failed
# assertion, which the script already reports.
AB_CMD_TIMEOUT="${AB_CMD_TIMEOUT:-45}"
eval_js() { timeout "${AB_CMD_TIMEOUT}" agent-browser eval "$1" 2>&1 | tail -1; }

# unquote <value> — removes the surrounding quotes agent-browser adds, so
# callers can compare against bare words instead of hand-writing "CLICKED".
unquote() { printf '%s' "${1}" | python3 -c 'import json,sys
raw = sys.stdin.read().strip()
try:
    print(json.loads(raw))
except Exception:
    print(raw)' 2>/dev/null || printf '%s' "${1}"; }

# wait_for <expected> <js> [attempts] — polls an expression until it matches.
# Fixed sleeps were flaky here: the app inventory arrives through an async
# detail fetch, so a screen can legitimately still be loading a moment after the
# navigation click. Polling distinguishes "slow" from "never renders".
# Both always exit 0: the script runs under `set -e`, so returning non-zero from
# a command substitution would abort the whole suite before the caller could
# report which assertion failed. The caller compares the printed value.
wait_for() {
  local expected="$1" js="$2" attempts="${3:-12}" i value=""
  for ((i = 0; i < attempts; i++)); do
    value="$(unquote "$(eval_js "${js}")")"
    if [ "${value}" = "${expected}" ]; then break; fi
    sleep 1
  done
  printf '%s' "${value}"
}

# wait_for_like <substring> <js> [attempts] — same, for substring matches.
wait_for_like() {
  local needle="$1" js="$2" attempts="${3:-12}" i value=""
  for ((i = 0; i < attempts; i++)); do
    value="$(unquote "$(eval_js "${js}")")"
    if [[ "${value}" == *"${needle}"* ]]; then break; fi
    sleep 1
  done
  printf '%s' "${value}"
}

# click_text <exact label> — clicks the button whose text matches exactly.
click_text() {
  eval_js "(()=>{const b=[...document.querySelectorAll('button')].find(x=>x.textContent.trim()===${1@Q});if(!b)return 'MISSING';if(b.disabled)return 'DISABLED';b.click();return 'CLICKED';})()"
}

# ---------------------------------------------------------------------------
section "Seed data through the API"
# ---------------------------------------------------------------------------

curl -fsS -o /dev/null "${BASE}/" || { echo "dashboard not reachable at ${BASE}; run 'make dev-d' first" >&2; exit 2; }

# The dev Vite server proxies /api, so the browser and the seed talk to the same
# origin the dashboard uses. Seeding against ${API} directly would exercise a
# different path than the one under test.
REGISTER=$(curl -fsS -c "${COOKIES}" -X POST "${BASE}/api/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${EMAIL}\",\"password\":\"${PASSWORD}\"}")
check "account created" "${REGISTER}" "owner_id"

# enroll-token and enroll take the token in the "token" field, not
# "enroll_token" — a first attempt with the wrong name fails with a bare SQL
# error that is easy to misread as a server fault.
ENROLL=$(curl -fsS -b "${COOKIES}" -X POST "${BASE}/api/agent/enroll-token" \
  -H 'Content-Type: application/json' -d '{}' | json_get "['token']")

ENROLLED=$(curl -fsS -b "${COOKIES}" -X POST "${BASE}/api/agent/enroll" \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"${ENROLL}\",\"hostname\":\"${HOSTNAME}\",\"user_id\":\"alice\",\"fingerprint\":\"e2e-$$-$(date +%s)\"}")
DEVICE_ID=$(printf '%s' "${ENROLLED}" | json_get "['device_id']")
DEVICE_TOKEN=$(printf '%s' "${ENROLLED}" | json_get "['device_token']")

if [ -n "${DEVICE_ID}" ] && [ -n "${DEVICE_TOKEN}" ]; then
  ok "device enrolled (${DEVICE_ID})"
else
  fail "device enrolled" "${ENROLLED}"
  exit 1
fi

# sshd is deliberately seeded with active_state "active" and sub_state "running",
# which is what systemd actually emits for a healthy service. If the agent-side
# status mapping ever regresses, the dashboard would show "active" instead of
# "Running" and this assertion catches it.
cat > "${WORK}/inventory.json" <<'JSON'
{
  "services": [
    {"name":"nginx.service","status":"failed","load_state":"loaded","active_state":"failed",
     "sub_state":"failed","unit_file_state":"enabled-runtime",
     "description":"A high performance web server","enabled":true},
    {"name":"sshd.service","status":"running","load_state":"loaded","active_state":"active",
     "sub_state":"running","unit_file_state":"enabled",
     "description":"OpenBSD Secure Shell server","enabled":true},
    {"name":"docker.service","status":"running","load_state":"loaded","active_state":"active",
     "sub_state":"running","unit_file_state":"enabled",
     "description":"Docker Application Container Engine","enabled":true},
    {"name":"bluetooth.service","status":"stopped","load_state":"loaded","active_state":"inactive",
     "sub_state":"dead","unit_file_state":"disabled",
     "description":"Bluetooth service","enabled":false}
  ],
  "ports": [
    {"protocol":"tcp","local_address":"","port":22,"process":"sshd","pid":812},
    {"protocol":"tcp","local_address":"127.0.0.1","port":8080,"process":"node","pid":90},
    {"protocol":"udp","local_address":"127.0.0.53","port":53,"process":"systemd-resolve","pid":750},
    {"protocol":"tcp","local_address":"","port":5432,"process":"postgres"}
  ]
}
JSON

# The device token is injected rather than hand-written into the JSON above so
# the seed data stays readable and the credential stays in exactly one place.
python3 - "${WORK}/inventory.json" "${DEVICE_TOKEN}" <<'PY'
import json, sys
path, token = sys.argv[1], sys.argv[2]
data = json.load(open(path))
data["device_token"] = token
json.dump(data, open(path, "w"))
PY

UPLOAD=$(curl -fsS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/devices/${DEVICE_ID}/system-inventory" \
  -H "Authorization: Bearer ${DEVICE_TOKEN}" -H 'Content-Type: application/json' \
  --data @"${WORK}/inventory.json")
check "inventory upload returns 204" "${UPLOAD}" "204"

READBACK=$(curl -fsS -b "${COOKIES}" "${BASE}/api/devices/${DEVICE_ID}/services")
check "services read back" "${READBACK}" "nginx.service"

# App inventory, so the Packages screen has rows to filter instead of only an
# empty state. A filter over nothing proves nothing.
#
# No ?action=install here: that query routes to handleAppInstall, which is gated
# behind EnableRemoteMutations and answers 503 by default. The plain POST is the
# agent's inventory upload.
cat > "${WORK}/apps.json" <<'JSON'
[
  {"source":"apt","name":"nginx","version":"1.24.0"},
  {"source":"apt","name":"curl","version":"8.5.0"},
  {"source":"flatpak","name":"org.mozilla.firefox","version":"121.0"},
  {"source":"pacman","name":"vim","version":"9.1"},
  {"source":"aur","name":"yay","version":"12.3.4"}
]
JSON
python3 - "${WORK}/apps.json" "${WORK}/apps-payload.json" "${DEVICE_TOKEN}" <<'PY'
import json, sys
src, dst, token = sys.argv[1], sys.argv[2], sys.argv[3]
json.dump({"device_token": token, "apps": json.load(open(src))}, open(dst, "w"))
PY
APPS_UPLOAD=$(curl -fsS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/devices/${DEVICE_ID}/apps" \
  -H "Authorization: Bearer ${DEVICE_TOKEN}" -H 'Content-Type: application/json' \
  --data @"${WORK}/apps-payload.json")
check "apps upload returns 204" "${APPS_UPLOAD}" "204"

# Minimal hardware snapshot. Without it Storage, Memory and CPU render only their
# empty states, so the sweep would be asserting on "nothing configured" rather
# than on the screen doing its job. One partition is enough to exercise the disk
# rendering and the SMART footnote that only appears alongside partitions.
cat > "${WORK}/telemetry.json" <<'JSON'
{
  "cpu_usage_percent": 12.5,
  "cpu_model": "e2e test cpu",
  "cpu_temperature": 42,
  "memory_used_bytes": 1073741824,
  "memory_total_bytes": 8589934592,
  "disk_read_rate": 1024,
  "disk_write_rate": 2048,
  "disk_partitions": [
    {"mount": "/", "device": "/dev/sda1", "total_bytes": 107374182400,
     "used_bytes": 64424509440, "free_bytes": 42949672960, "used_percent": 60}
  ],
  "load_average": "0.42",
  "uptime_seconds": 86400
}
JSON
TELEMETRY=$(curl -fsS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/devices/${DEVICE_ID}/telemetry" \
  -H "Authorization: Bearer ${DEVICE_TOKEN}" -H 'Content-Type: application/json' \
  -d "{\"device_token\":\"${DEVICE_TOKEN}\",\"hardware\":$(cat "${WORK}/telemetry.json")}")
check "telemetry upload accepted" "${TELEMETRY}" "2"


HEARTBEAT=$(curl -fsS -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/devices/${DEVICE_ID}/heartbeat" \
  -H "Authorization: Bearer ${DEVICE_TOKEN}" -H 'Content-Type: application/json' \
  -d "{\"device_token\":\"${DEVICE_TOKEN}\"}")
check "heartbeat accepted" "${HEARTBEAT}" "200"

# ---------------------------------------------------------------------------
section "Sign in"
# ---------------------------------------------------------------------------

# Close any browser left over from a previous run. A live session carries its own
# cookies and its own launch flags: reusing it skips the login screen entirely,
# and --args is silently ignored while a daemon is already up, so the sandbox
# flag would not be applied either.
agent-browser close --all >/dev/null 2>&1 || true
sleep 1

timeout 60 agent-browser open "${BASE}/" --args "--no-sandbox" >/dev/null
sleep 3

if eval_js "!!document.querySelector('input[type=email]')" | grep -q true; then
  ok "login screen rendered"
else
  fail "login screen rendered" "no email field found"
fi

eval_js "(()=>{const s=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set;
 s.call(document.querySelector('input[type=email]'),${EMAIL@Q});
 document.querySelector('input[type=email]').dispatchEvent(new Event('input',{bubbles:true}));
 s.call(document.querySelector('input[type=password]'),${PASSWORD@Q});
 document.querySelector('input[type=password]').dispatchEvent(new Event('input',{bubbles:true}));
 return 'filled';})()" >/dev/null
sleep 1
click_text "Sign in" >/dev/null
sleep 4

check "signed in" "$(eval_js "document.body.textContent.includes('Fleet') ? 'Fleet' : document.body.textContent.slice(0,60)")" "Fleet"

# ---------------------------------------------------------------------------
section "Device selection enables the scoped sidebar"
# ---------------------------------------------------------------------------

# The fleet row is a clickable element, not a button with a stable label. Its
# aria name includes the status text, so it is matched on hostname only.
ROW=$(eval_js "(()=>{const r=[...document.querySelectorAll('tr,[role=row]')].find(x=>x.textContent.includes(${HOSTNAME@Q}));if(!r)return 'MISSING';r.click();return 'CLICKED';})()")
check "fleet row clicked" "${ROW}" "CLICKED"
sleep 3

# Every device-scoped item is disabled until a device is selected. If this fails
# the dashboard is unusable for per-device work, which is most of it.
SCOPED=$(eval_js "(()=>{const b=[...document.querySelectorAll('button')].find(x=>x.textContent.trim()==='Services');return b?(b.disabled?'DISABLED':'ENABLED'):'MISSING';})()")
check "Services nav enabled after selecting a device" "${SCOPED}" "ENABLED"

# ---------------------------------------------------------------------------
section "Services screen"
# ---------------------------------------------------------------------------

check "navigate to Services" "$(click_text 'Services')" "CLICKED"
sleep 3

NAMES=$(eval_js "[...document.querySelectorAll('.list li .name')].map(e=>e.textContent.trim()).join(',')")
check "four services rendered" "${NAMES}" "nginx.service,sshd.service,docker.service,bluetooth.service"

ROW_TEXT=$(eval_js "(()=>{const r=[...document.querySelectorAll('.list li')].find(x=>x.textContent.includes('sshd.service'));return r?r.textContent.replace(/\s+/g,' '):'';})()")
# The label is capitalised by CSS text-transform, which textContent does not
# reflect, so the underlying value is asserted rather than the rendered casing.
check "sshd shows the mapped status, not the raw systemd column" "${ROW_TEXT}" "running"
if [[ "${ROW_TEXT}" == *"active"* ]]; then
  fail "raw active_state must not be displayed" "${ROW_TEXT}"
else
  ok "raw active_state is not shown"
fi

NGINX=$(eval_js "(()=>{const r=[...document.querySelectorAll('.list li')].find(x=>x.textContent.includes('nginx.service'));return r?r.textContent.replace(/\s+/g,' '):'';})()")
check "failed unit keeps its real unit file state" "${NGINX}" "enabled-runtime"

check "failed/running summary" "$(eval_js "document.querySelector('.summary')?.textContent.replace(/\s+/g,' ').trim()")" "1 failed"

PORTS=$(eval_js "(()=>{const r=[...document.querySelectorAll('.ports li')].map(e=>e.textContent.replace(/\s+/g,' ').trim());return r.length+' rows';})()")
check "four ports rendered" "${PORTS}" "4 rows"

# A wildcard listener arrives with an empty local_address; showing an empty cell
# would read as missing data rather than "bound to all interfaces".
check "wildcard listener renders as *" "$(eval_js "(()=>{const r=[...document.querySelectorAll('.ports li')].find(x=>x.textContent.includes('5432'));return r?r.textContent.replace(/\s+/g,' '):'';})()")" "* 5432 postgres"

# ---------------------------------------------------------------------------
section "Filters"
# ---------------------------------------------------------------------------

# Set the filter by aria-label. A bare querySelector('input[type=search]')
# silently targets the device search box, which appears earlier in the DOM — that
# mistake cost a manual debugging cycle and would hide a real filter bug here.
filter_box() {
  eval_js "(()=>{const i=[...document.querySelectorAll('input[type=search]')].find(x=>x.getAttribute('aria-label')==${1@Q});
   if(!i)return 'MISSING';
   const s=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set;
   s.call(i,${2@Q});i.dispatchEvent(new Event('input',{bubbles:true}));return 'SET';})()"
}

filter_box "Filter services" "docker" >/dev/null
sleep 2
check "service filter narrows the list" "$(eval_js "[...document.querySelectorAll('.list li .name')].map(e=>e.textContent.trim()).join(',')")" "docker.service"

filter_box "Filter services" "" >/dev/null
sleep 2

eval_js "(()=>{const c=document.querySelector('input[type=checkbox]');if(!c)return 'MISSING';c.click();return 'CLICKED';})()" >/dev/null
sleep 2
PROBLEMS=$(eval_js "[...document.querySelectorAll('.list li .name')].map(e=>e.textContent.trim()).join(',')")
check "problems only hides running units" "${PROBLEMS}" "nginx.service,bluetooth.service"

eval_js "document.querySelector('input[type=checkbox]').click()" >/dev/null
sleep 2

filter_box "Filter ports" "postgres" >/dev/null
sleep 2
check "port filter narrows the list" "$(eval_js "[...document.querySelectorAll('.ports li')].map(e=>e.textContent.replace(/\s+/g,' ').trim()).join(',')")" "tcp * 5432 postgres"

filter_box "Filter ports" "" >/dev/null
sleep 1

# ---------------------------------------------------------------------------
section "Failure state"
# ---------------------------------------------------------------------------

# A failed request must read as a failure. Rendering "no services" here would be
# indistinguishable from a device that genuinely reports nothing.
eval_js "(()=>{window.fetch=()=>Promise.reject(new TypeError('Failed to fetch'));return 'poisoned';})()" >/dev/null
eval_js "(()=>{const b=[...document.querySelectorAll('button')].find(x=>/refresh/i.test(x.textContent));if(!b)return 'MISSING';b.click();return 'CLICKED';})()" >/dev/null
sleep 3

check "error banner shown" "$(eval_js "document.querySelector('[role=alert]')?.textContent || ''")" "Failed to fetch"
# EmptyState renders its title in a <strong>, so "Could not load services" is
# what proves the screen reported a failure instead of an empty inventory.
check "error is not reported as an empty inventory" "$(eval_js "[...document.querySelectorAll('.empty-state strong')].map(e=>e.textContent).join(',')")" "Could not load services"

# Recovery: after a clean reload the screen must show the data again.
timeout "${AB_CMD_TIMEOUT}" agent-browser reload >/dev/null
sleep 4
ROW=$(eval_js "(()=>{const r=[...document.querySelectorAll('tr,[role=row]')].find(x=>x.textContent.includes(${HOSTNAME@Q}));if(!r)return 'MISSING';r.click();return 'CLICKED';})()")
sleep 3
click_text 'Services' >/dev/null
sleep 3
check "services recover after reload" "$(eval_js "[...document.querySelectorAll('.list li .name')].map(e=>e.textContent.trim()).join(',')")" "nginx.service,sshd.service,docker.service,bluetooth.service"

# ---------------------------------------------------------------------------
section "Every sidebar section renders"
# ---------------------------------------------------------------------------

# The redesign built ten screens and none of them had been opened before. Each
# section is visited and checked for two failure modes that type-checking cannot
# catch: the screen rendering nothing at all, and a section that is reachable in
# the sidebar but throws or blanks out at runtime.
FLEET_SECTIONS=("Home" "Devices" "Alerts" "Findings" "Reports")
DEVICE_SECTIONS=("Overview" "CPU & Thermal" "Memory" "Storage" "Network" "All sensors" \
                 "Containers" "Processes" "Services" "Packages" "Logs" "Security" \
                 "Remote actions" "Settings")

# Re-select the device after the reload above, which drops the selection.
eval_js "(()=>{const r=[...document.querySelectorAll('tr,[role=row]')].find(x=>x.textContent.includes(${HOSTNAME@Q}));if(!r)return 'MISSING';r.click();return 'CLICKED';})()" >/dev/null
sleep 2

panel_text() {
  eval_js "(()=>{const m=document.querySelector('main')||document.body;const t=m.innerText.replace(/\s+/g,' ').trim();return t.length?t:'EMPTY';})()"
}

sweep_section() {
  local label="$1" nav="$2" min_chars="${3:-60}"
  local result
  result=$(unquote "$(click_text "${nav}")")
  if [ "${result}" != "CLICKED" ]; then
    fail "section '${nav}' reachable" "${result}"
    return
  fi
  sleep 2
  local body
  body=$(unquote "$(panel_text)")
  if [ "${body}" = "EMPTY" ] || [ -z "${body}" ]; then
    fail "section '${label}' renders content" "panel was empty after clicking '${nav}'"
    return
  fi
  if [ "${#body}" -lt "${min_chars}" ]; then
    fail "section '${label}' renders content" "only ${#body} chars: ${body}"
    return
  fi
  # A screen that throws during render usually leaves a bare error shell behind.
  if [[ "${body}" == *"Internal Server Error"* || "${body}" == *"undefined is not"* ]]; then
    fail "section '${label}' renders without a runtime error" "${body}"
    return
  fi
  ok "section '${label}' renders (${#body} chars)"
}

for nav in "${FLEET_SECTIONS[@]}"; do
  sweep_section "${nav}" "${nav}" 40
done

for nav in "${DEVICE_SECTIONS[@]}"; do
  sweep_section "${nav}" "${nav}" 60
done

# Distinct sections that render byte-identical panels are the known debt in
# context/dashboard-redesign.md: cpu, memory, network and sensors all fall
# through to DeviceOverview, and findings duplicates alerts. Reported rather than
# failed, because the product decision (own screens vs. removing the redundant
# sidebar entries) has not been made yet. If it is made either way, replace this
# with a hard assertion.
DUPLICATE_REPORT="${WORK}/duplicates.txt"
: > "${DUPLICATE_REPORT}"
render_fingerprint() {
  click_text "$1" >/dev/null
  sleep 2
  unquote "$(eval_js "(()=>{const m=document.querySelector('main')||document.body;return m.innerText.replace(/\s+/g,' ').trim();})()")"
}

for nav in "${DEVICE_SECTIONS[@]}"; do
  printf '%s\t%s\n' "${nav}" "$(render_fingerprint "${nav}" | cksum | cut -d' ' -f1)" >> "${DUPLICATE_REPORT}"
done

DUPS=$(sort -k2 "${DUPLICATE_REPORT}" | awk -F'\t' '{c[$2]=c[$2]" "$1; n[$2]++} END {for (k in n) if (n[k]>1) print "   -"c[k]}')

if [ -n "${DUPS}" ]; then
  printf '  note  sections rendering identical panels (known debt):\n%s\n' "${DUPS}"
  ok "duplicate-section report produced"
else
  ok "every device section renders a distinct panel"
fi

# ---------------------------------------------------------------------------
section "Screen interactions"
# ---------------------------------------------------------------------------

# The sweep above only proves each screen renders. These checks exercise the
# controls, because a control that renders but does nothing is exactly the class
# of fault that type-checking cannot see.

# Packages: search and source filter over the seeded inventory.
click_text 'Packages' >/dev/null
sleep 2

# The inventory arrives through an async detail fetch, so poll rather than
# sleeping a fixed amount.
PACKAGES_SEEDED=$(wait_for_like "3" "(()=>{const t=document.body.innerText;return String(['nginx','curl','org.mozilla.firefox','vim'].filter(n=>t.includes(n)).length);})()" 15)
if [ "${PACKAGES_SEEDED}" -ge 3 ]; then
  ok "packages inventory rendered (${PACKAGES_SEEDED}/4 seeded apps visible)"
else
  fail "packages inventory rendered" "only ${PACKAGES_SEEDED} seeded apps visible"
fi

filter_box "Filter packages" "vim" >/dev/null
sleep 2
PACKAGES_FILTERED=$(unquote "$(eval_js "(()=>{const t=document.body.innerText;return t.includes('vim')?'has-vim':'no-vim';})()")")
check "package search matches a seeded app" "${PACKAGES_FILTERED}" "has-vim"

filter_box "Filter packages" "zzzznope" >/dev/null
sleep 2
# "No packages reported" and "Not collected yet" are different cards and both
# appear on this screen, so the match state has to be asserted specifically.
PACKAGES_EMPTY=$(unquote "$(eval_js "(()=>{const b=[...document.querySelectorAll('.empty-state strong')].map(e=>e.textContent);return b.includes('No match')?'No match':b.join(',');})()")")
check "package search with no match shows the match state" "${PACKAGES_EMPTY}" "No match"

filter_box "Filter packages" "" >/dev/null
sleep 1

SOURCE_SET=$(eval_js "(()=>{const s=[...document.querySelectorAll('select')].find(x=>x.getAttribute('aria-label')==='Filter by source');if(!s)return 'MISSING';s.value='pacman';s.dispatchEvent(new Event('change',{bubbles:true}));return 'SET';})()")
check "package source filter present" "$(unquote "${SOURCE_SET}")" "SET"
sleep 2
PACKAGES_SOURCE=$(unquote "$(eval_js "(()=>{const t=document.body.innerText;return (t.includes('vim')?'has-vim:':'')+(t.includes('firefox')?'has-firefox':'no-firefox');})()")")
check "package source filter narrows to one source" "${PACKAGES_SOURCE}" "has-vim:no-firefox"

eval_js "(()=>{const s=[...document.querySelectorAll('select')].find(x=>x.getAttribute('aria-label')==='Filter by source');s.value='all';s.dispatchEvent(new Event('change',{bubbles:true}));return 'RESET';})()" >/dev/null
sleep 1

# Processes: sort controls must exist and switch the ordering.
click_text 'Processes' >/dev/null
sleep 2
# The controls read "By CPU" / "By memory"; the active-state class is hashed by
# Svelte, so matching on class names does not work.
PROCESS_SORT=$(eval_js "(()=>{const b=[...document.querySelectorAll('button')].find(x=>x.textContent.trim()==='By CPU');return b?'PRESENT':'ABSENT';})()")
check "processes screen exposes a sort control" "$(unquote "${PROCESS_SORT}")" "PRESENT"

# Alerts: the reset control appears only when a filter is active.
click_text 'Alerts' >/dev/null
sleep 2
ALERTS_RESET=$(eval_js "(()=>{const b=[...document.querySelectorAll('button')].find(x=>x.textContent.trim()==='All');return b?'PRESENT':'ABSENT';})()")
if [ "$(unquote "${ALERTS_RESET}")" = "PRESENT" ]; then
  ok "alerts screen exposes its severity filter"
else
  fail "alerts screen exposes its severity filter" "no 'All' severity control"
fi

# Storage: this screen is documented as honest about missing SMART data. It must
# name the missing endpoint rather than showing a fake health row.
click_text 'Storage' >/dev/null
sleep 2
STORAGE_TEXT=$(unquote "$(eval_js "(()=>{const m=document.querySelector('main')||document.body;return m.innerText.replace(/\s+/g,' ');})()")")
if [[ "${STORAGE_TEXT}" == *"SMART"* || "${STORAGE_TEXT}" == *"smart"* ]]; then
  ok "storage screen names the missing SMART data"
else
  fail "storage screen names the missing SMART data" "no SMART reference in: ${STORAGE_TEXT:0:120}"
fi

# Remote actions: the three actions are real now, so the screen must list them
# and state the conditions (server flag + local policy) instead of claiming the
# actions are disabled.
click_text 'Remote actions' >/dev/null
sleep 2
REMOTE_TEXT=$(unquote "$(eval_js "(()=>{const m=document.querySelector('main')||document.body;return m.innerText.replace(/\s+/g,' ');})()")")
if [[ "${REMOTE_TEXT}" == *"Restart agent"* && "${REMOTE_TEXT}" == *"Apply package updates"* && "${REMOTE_TEXT}" == *"Reboot device"* && "${REMOTE_TEXT}" == *"local policy"* ]]; then
  ok "remote actions screen lists the actions and their policy conditions"
else
  fail "remote actions screen lists the actions and their policy conditions" "${REMOTE_TEXT:0:200}"
fi

# ---------------------------------------------------------------------------
section "Fleet overview"
# ---------------------------------------------------------------------------

click_text 'Home' >/dev/null
sleep 3

# The fleet stat cards sit in one grid row. If any card overflows its row the
# grid becomes taller than the viewport area it was allotted and the device
# table drops below the fold, which is a visible layout fault that no static
# check catches.
ALIGN=$(eval_js "(()=>{const t=document.body.innerText;return t.includes('Fleet')?'Fleet':'missing';})()")
check "fleet view rendered" "${ALIGN}" "Fleet"

# The fleet stat cards share one grid row, so their boxes must line up. A card
# whose sub-label wraps to more lines than its neighbours grows taller and drops
# the device table below the fold. This was visibly broken — the "Without Lynis"
# card overflowed its row — while vite build and svelte-check were both clean,
# which is the whole reason this suite exists.
KPI=$(eval_js "(()=>{const c=[...document.querySelectorAll('.kpi')];
 if(c.length<2) return JSON.stringify({error:'found '+c.length+' cards'});
 const boxes=c.map(x=>{const r=x.getBoundingClientRect();return {top:Math.round(r.top),bottom:Math.round(r.bottom),h:Math.round(r.height)};});
 const tops=new Set(boxes.map(b=>b.top));
 return JSON.stringify({count:c.length,tops:[...tops],bottoms:boxes.map(b=>b.bottom),heights:boxes.map(b=>b.h)});})()")

# agent-browser prints the return value as a JSON string, so the payload is
# encoded twice. Decoding once yields a quoted string, so it has to be unwrapped
# twice before the keys are readable.
kpi_field() {
  printf '%s' "${KPI}" | python3 -c "
import json, sys
raw = sys.stdin.read().strip()
try:
    data = json.loads(json.loads(raw))
except Exception:
    print('unparsed'); raise SystemExit
print($1)
" 2>/dev/null || echo "unparsed"
}

KPI_COUNT=$(kpi_field "data.get('count', 0)")
KPI_TOPS=$(kpi_field "len(data.get('tops', []))")
KPI_BOTTOMS=$(kpi_field "'ALIGNED' if data.get('bottoms') and len(set(data['bottoms'])) == 1 else 'MISALIGNED:' + str(data.get('bottoms'))")

if [ "${KPI_COUNT}" -ge 2 ]; then
  ok "fleet KPI cards rendered (${KPI_COUNT})"
else
  fail "fleet KPI cards rendered" "${KPI}"
fi

if [ "${KPI_TOPS}" -le 1 ]; then
  ok "KPI cards sit on one row"
else
  fail "KPI cards must share a single row" "distinct top edges: ${KPI_TOPS}"
fi

check "KPI cards share a bottom edge" "${KPI_BOTTOMS}" "ALIGNED"

# ---------------------------------------------------------------------------
printf '\n== %s\n' "Result"
if [ "${FAILURES}" -eq 0 ]; then
  printf 'All %s checks passed.\n' "${CHECKS}"
  exit 0
fi
printf '%s of %s checks failed.\n' "${FAILURES}" "${CHECKS}"
exit 1