# Changelog

All notable, user-facing changes to SyncWin are documented here. The Release
workflow reads the section matching the version it is about to publish and uses
it as the GitHub Release body. A missing or empty section stops the release
before the image is built, so a tag always carries written notes.

Versions are decided by release-please: it reads the Conventional Commits on
`main` and opens a pull request with the next version and the section below.
`feat:` increases the minor version, everything else increases the patch
version, and a breaking change increases the major version. Merging that pull
request publishes the image and creates the tag.

## [0.18.0](https://github.com/jose-guilherme93/sync-win/compare/v0.17.0...v0.18.0) (2026-11-16)


### Features

* accurate network rates, per-process CPU deltas, and dashboard overhaul ([3e9324d](https://github.com/jose-guilherme93/sync-win/commit/3e9324da82bd8b9935ea835b115458091c8892d2))
* add OpenPencil UI prototype workflow ([c425931](https://github.com/jose-guilherme93/sync-win/commit/c425931c060d6190b0e86c56b8eb4c6390cc12f4))
* add ponytail skills and commands for over-engineering audits and metrics ([328c79c](https://github.com/jose-guilherme93/sync-win/commit/328c79cdc63118432669ddb4fd60140377fd1e0f))
* add project workspace config sync (P1.6) ([28074f2](https://github.com/jose-guilherme93/sync-win/commit/28074f27ca68059598d94986ce1b1808c10d3a47))
* **agent:** collect CPU clock and core voltage ([3f6c70b](https://github.com/jose-guilherme93/sync-win/commit/3f6c70bc2b64d8547af560a06412241e4b52729d))
* **agent:** expose the fingerprint and adopt machine-less records ([320f17c](https://github.com/jose-guilherme93/sync-win/commit/320f17c1391cb389d64c08b8e8e048f336141f24))
* **agent:** remote actions and pending updates collector ([1f37fe6](https://github.com/jose-guilherme93/sync-win/commit/1f37fe6017fc55f71107b866d023e79b427aec19))
* **auth:** disable public registration by default, bootstrap admin from env ([9a38a40](https://github.com/jose-guilherme93/sync-win/commit/9a38a405d59b35e65d420013bb76b948b94b1c15))
* authenticate agent auto-updates with Ed25519 signatures ([36f4aa4](https://github.com/jose-guilherme93/sync-win/commit/36f4aa43315856b6709ef68bc4b9010b61b6ef31))
* **auth:** open the dashboard with a token from the environment ([7057ff8](https://github.com/jose-guilherme93/sync-win/commit/7057ff8d0652a10ec884ef86afbe1b7ad8684165))
* brand mark, token login, device screens, docker images and live telemetry ([63a873a](https://github.com/jose-guilherme93/sync-win/commit/63a873a67ec9c0456b1bb3ccb8fef2970e1634df))
* **ci:** automatic semantic versioning on every push to main ([78fa582](https://github.com/jose-guilherme93/sync-win/commit/78fa5828f9a5511c50795ef2a19cd08677743267))
* **ci:** keep both deployment paths and publish v-prefixed image tags ([754baad](https://github.com/jose-guilherme93/sync-win/commit/754baad8dbffd1fa5251c0042d7bdcf805319e6c))
* **ci:** keep both deployment paths working and verify them ([0946503](https://github.com/jose-guilherme93/sync-win/commit/0946503deb19d4f36f175cd70564b2f7bbe6937b))
* collect systemd services and listening ports from the agent ([34e9f15](https://github.com/jose-guilherme93/sync-win/commit/34e9f15588de9e93dad7d8de047d12f9961fcd45))
* complete saves sync and agent auto-update (P1.3/P1.4) ([e4cc6b2](https://github.com/jose-guilherme93/sync-win/commit/e4cc6b21e14a90ff979ded3281b6d845c2d5f206))
* connect agent application inventory (P1.2) ([064662a](https://github.com/jose-guilherme93/sync-win/commit/064662ada66ba79853e34948e535393539814433))
* connect agent preference sync (P1.1) ([3394670](https://github.com/jose-guilherme93/sync-win/commit/33946708a96f4d4acda07ba18b0e4d3c8f3bef6e))
* **dev:** give the full test device a working remote-access session ([e7832c0](https://github.com/jose-guilherme93/sync-win/commit/e7832c0cab1f2a8c076c9d9d34dbc5b6486220fb))
* **dev:** sign the dev agent so the dev stack exercises auto-update ([f049959](https://github.com/jose-guilherme93/sync-win/commit/f049959bd231eb6c7b3566cbf8d7e4eee0cba0f8))
* **dev:** sign the dev stack in with a fixed account and token ([c23d77d](https://github.com/jose-guilherme93/sync-win/commit/c23d77d7d05201cd5be4c499171616e4e1288cc2))
* **docker:** expand containers inline with live metrics and logs ([662c243](https://github.com/jose-guilherme93/sync-win/commit/662c24344bd9124113ddc135d80565e60e7b4bf3))
* **docker:** manage images, and sample telemetry every two seconds ([f2f4dd1](https://github.com/jose-guilherme93/sync-win/commit/f2f4dd11680c3387452361a7a12a521910b5ccad))
* **docker:** rebuild the screen around live state and a fast agent queue ([a91523a](https://github.com/jose-guilherme93/sync-win/commit/a91523ae3e4527e525bdae25295840aec711dfa9))
* implement package installation actions (P1.5) ([8f9a0a2](https://github.com/jose-guilherme93/sync-win/commit/8f9a0a281c938c50c61cf4cc19590d9776082c5e))
* implement processes tab backend ([88a52fd](https://github.com/jose-guilherme93/sync-win/commit/88a52fde60adc4a28f0e915ab6fc04ff81915bb9))
* implement Processes tab web UI (variant A + tree toggle) ([5f3fd04](https://github.com/jose-guilherme93/sync-win/commit/5f3fd04db4298e26e0c03816ec4f0f26edd51cc5))
* **installer:** ask for capabilities and accept --yes ([8365642](https://github.com/jose-guilherme93/sync-win/commit/8365642a58e15e637933a9a6ada4d7e097485d28))
* **installer:** ask for capabilities and accept --yes ([2110335](https://github.com/jose-guilherme93/sync-win/commit/2110335a8afd06ba4dd6c3a7d10171cf3d8b8015))
* **installer:** ask for capabilities and accept --yes ([ba6dbc5](https://github.com/jose-guilherme93/sync-win/commit/ba6dbc533d4c898c0bc0ffa7d41a0af54b061e3f))
* **installer:** offer to install Lynis for security audits, defaulting to yes ([88af4da](https://github.com/jose-guilherme93/sync-win/commit/88af4daa5a700447de41dd713fa41b622fbbb45f))
* **logs:** stream application logs over SSE ([a6432f4](https://github.com/jose-guilherme93/sync-win/commit/a6432f456f3b5e46951a2e6e5e077c639da5d6d2))
* **network:** add an interactive network topology map ([c29a1a5](https://github.com/jose-guilherme93/sync-win/commit/c29a1a5db6debec8d58fa7650f215edd327ed753))
* **network:** add an interactive network topology map ([4956683](https://github.com/jose-guilherme93/sync-win/commit/4956683830f73cf0e015bd62e72e630a022c4e76))
* **network:** collect the whole network view from the device ([5099325](https://github.com/jose-guilherme93/sync-win/commit/5099325ae2edab2d24f2d4094f96623709128151))
* **network:** let the operator arrange the panels ([b673d72](https://github.com/jose-guilherme93/sync-win/commit/b673d72c76ab6a93e5391f4080ccc0fe13d0afc4))
* **network:** let the operator arrange the panels ([1a0d465](https://github.com/jose-guilherme93/sync-win/commit/1a0d4656812888d1a9e86a60bc5381c736fde8fc))
* **network:** open a stat tile into a live inspector ([28eaf89](https://github.com/jose-guilherme93/sync-win/commit/28eaf89d81778cac2d6ccc712b72d419fefa6485))
* **network:** rebuild the screen around what an operator needs ([3abff5f](https://github.com/jose-guilherme93/sync-win/commit/3abff5f98fa87a09cd5c9bfc522c1e48ffa95d11))
* **network:** the whole network view, collected and displayed ([bbdb536](https://github.com/jose-guilherme93/sync-win/commit/bbdb5364fef1765f563a12a817b188bdcb22abcb))
* **notifications:** add a notification center with per-event preferences and sound ([c13f482](https://github.com/jose-guilherme93/sync-win/commit/c13f48258f1f7839e31d1872b726af0295c02459))
* **notifications:** an inbox that reads like an inbox ([aca9a00](https://github.com/jose-guilherme93/sync-win/commit/aca9a007d19b2acf2e015a88dc4d24a2867066f8))
* **overview:** turn the device Overview into a full dashboard ([2a1cded](https://github.com/jose-guilherme93/sync-win/commit/2a1cdede06a00347388b3398ca7eaffeeb6f762d))
* persist alert acknowledgements per account ([26f8450](https://github.com/jose-guilherme93/sync-win/commit/26f84509a9cda9f0b692991802057750af42b70a))
* process diagnostics and a network inspector from feat/terminal-tabs ([c681119](https://github.com/jose-guilherme93/sync-win/commit/c681119e88c47ead99759d9bb8b1b9900a2b2a09))
* Processes tab — live table, tree, kill actions, history ([9741c7e](https://github.com/jose-guilherme93/sync-win/commit/9741c7ed3f5801437677cb92099e42e256a5d6b6))
* **processes:** diagnose the machine with states, identity and detail ([e276fba](https://github.com/jose-guilherme93/sync-win/commit/e276fbafd50dcdb059429ea9a6841fcf8443372b))
* **processes:** live table, guarded signals, process history and IO ([a1f6ca5](https://github.com/jose-guilherme93/sync-win/commit/a1f6ca5637ab206bf6a1709e27bcd4a975a44c9d))
* **remote-access:** opt-in SSH terminal over the agent tunnel ([7bd828d](https://github.com/jose-guilherme93/sync-win/commit/7bd828d4944d8508b0b97344eec732c9f9299096))
* **remote-access:** opt-in SSH terminal over the agent tunnel ([e87a7b2](https://github.com/jose-guilherme93/sync-win/commit/e87a7b23b8ab2bd66c9392cc774d6b7bb0727a59))
* **remote-access:** opt-in SSH terminal over the agent tunnel ([ebbb1e4](https://github.com/jose-guilherme93/sync-win/commit/ebbb1e44c59d16c959e64abc30af0bbd7ce0e66b))
* **remote-access:** run up to three terminals in tabs ([1a7dd40](https://github.com/jose-guilherme93/sync-win/commit/1a7dd40fe5d8a4e518dc87aeacd5d28ab0875d37))
* **remote-access:** six tabs named by the running program ([2a9f98e](https://github.com/jose-guilherme93/sync-win/commit/2a9f98ed6a494127420a7c56df8df9a57d419150))
* **server:** device settings, remote actions and pending updates ([c5d0b34](https://github.com/jose-guilherme93/sync-win/commit/c5d0b343f099c5dd1c5cfdb87690e90bb88d7410))
* **server:** make log level and retention configurable at runtime ([4f785f5](https://github.com/jose-guilherme93/sync-win/commit/4f785f56c42d538c5449d506109420a1dc0e4629))
* system inventory, browser e2e suite and Packages fix ([7ae1a6e](https://github.com/jose-guilherme93/sync-win/commit/7ae1a6ecef66cae0bc535e5212c0360197f54804))
* **web:** add app shell with sidebar navigation and shared UI primitives ([c64bde7](https://github.com/jose-guilherme93/sync-win/commit/c64bde7ebd85e3392770b0564e9153fafea91125))
* **web:** add device overview screen with gauges and history ([32f890a](https://github.com/jose-guilherme93/sync-win/commit/32f890a60d8598cb15b33234396173795b34e6c9))
* **web:** add fleet Home screen and wire it into the shell ([55d9bf6](https://github.com/jose-guilherme93/sync-win/commit/55d9bf62d98f9ea04fbab2036d60a733f8774200))
* **web:** add the remaining fleet and device screens ([759c313](https://github.com/jose-guilherme93/sync-win/commit/759c313620033f6f39d8411ffe344ae66e03ba49))
* **web:** application logs in a fleet-wide Settings screen ([84fe721](https://github.com/jose-guilherme93/sync-win/commit/84fe72125282bd12c0045c31f823396f16374dc5))
* **web:** Beszel-style time-range selector for telemetry charts ([b283fef](https://github.com/jose-guilherme93/sync-win/commit/b283fefd999f8b1948a44d5084e189a813826e75))
* **web:** dedicated CPU & Thermal, Memory and All sensors screens ([937498d](https://github.com/jose-guilherme93/sync-win/commit/937498d6b1646b10162851dede6d4a42123d4dd2))
* **web:** dedicated network, storage, services, security and docker screens ([4317f52](https://github.com/jose-guilherme93/sync-win/commit/4317f52d363fc682cca7570112c9a72c508e30d2))
* **web:** device log viewer with filtering, search and paging ([9dc99e9](https://github.com/jose-guilherme93/sync-win/commit/9dc99e9669b204bf7497302996d87a86cad41a02))
* **web:** filter application logs by request id ([e3acf94](https://github.com/jose-guilherme93/sync-win/commit/e3acf94fb18d9839da69d68262b23e7181051821))
* **web:** give the device screen its own tabs instead of a sidebar menu ([1f48715](https://github.com/jose-guilherme93/sync-win/commit/1f487155a370e8ddbbab9344661cebcec015c2f1))
* **web:** icon-only chrome, loading skeletons and a stream-like Live chart ([edc7469](https://github.com/jose-guilherme93/sync-win/commit/edc74699b5b733adbfed36e7a38748b242e1d2cb))
* **web:** load real history for the overview period selector ([b07185c](https://github.com/jose-guilherme93/sync-win/commit/b07185cf51e0f0310cffebded5ee8bf3e1cc1bf7))
* **web:** move telemetry retention into Settings ([b2db096](https://github.com/jose-guilherme93/sync-win/commit/b2db096903e3a3f780558b08b4f8bb22acdaea47))
* **web:** rich Lynis security screen with mitigation actions ([e2da810](https://github.com/jose-guilherme93/sync-win/commit/e2da810db3de0be8927a69d3e2440cea9fe20143))
* **web:** turn the fleet landing into a host list with inline usage meters ([167bbbb](https://github.com/jose-guilherme93/sync-win/commit/167bbbb898531b42b0b0d6d3625f72d1b63c5df2))
* **web:** wire settings, remote actions, pending updates and Findings ([6012644](https://github.com/jose-guilherme93/sync-win/commit/6012644e36fe6af0bdae6d63c0193bf024d32d89))
* **web:** wire the Services screen to the new endpoints ([9a520a4](https://github.com/jose-guilherme93/sync-win/commit/9a520a4cc83e295eca0b0011ce1dd180e458aa98))


### Bug Fixes

* **agent:** advertise 0.11.0 so devices receive the topology collector ([13d8b74](https://github.com/jose-guilherme93/sync-win/commit/13d8b748aa5a366fb06c1455095f393c05d24dd3))
* **agent:** advertise 0.11.0 so devices receive the topology collector ([e50a45b](https://github.com/jose-guilherme93/sync-win/commit/e50a45b02fcbbac7620ba6cc0ee215e7afb7a9c2))
* **agent:** bound cleanup commands with a context ([7fc659f](https://github.com/jose-guilherme93/sync-win/commit/7fc659fd231544eca471b18f38b47340235aa499))
* **agent:** cap docker logs tail via the collection contract ([7c04bfb](https://github.com/jose-guilherme93/sync-win/commit/7c04bfb127b8fc0fe92d2b8d22904ef39d869405))
* **agent:** clean legacy artifacts and installer backups ([f005f32](https://github.com/jose-guilherme93/sync-win/commit/f005f324c956160c3de0d64ae1f35caf0b8f909e))
* **agent:** collector edge cases and updater timeouts ([a70c78f](https://github.com/jose-guilherme93/sync-win/commit/a70c78fb56a2a30b2e92e544566e990510079d9d))
* **agent:** confine restores to save roots and honor excluded files ([81435dc](https://github.com/jose-guilherme93/sync-win/commit/81435dc860557338d896b447b75ab8f8b0027aa5))
* **agent:** contract-driven secret checks, redacted logs, correct disk IO ([3bee9fc](https://github.com/jose-guilherme93/sync-win/commit/3bee9fc06c4bd30443c70916ba80de594d44bda1))
* **agent:** grant journal read access so device logs are collected at all ([91d7083](https://github.com/jose-guilherme93/sync-win/commit/91d7083ae2712a3032a55567c091c900f179c9c1))
* **agent:** harden compose write against symlink escape and partial writes ([4401c84](https://github.com/jose-guilherme93/sync-win/commit/4401c84659cb6bcf2c8037b2567ac9c121a432e7))
* **agent:** let the daemon request its own update ([ab8053d](https://github.com/jose-guilherme93/sync-win/commit/ab8053d70f76129721454fd8a0db4af70800b6ff))
* **agent:** make a device fix reach machines that are already installed ([0f6aeb0](https://github.com/jose-guilherme93/sync-win/commit/0f6aeb06fe2fb30f2f76c20db672a1dcf3966040))
* **agent:** parse the Lynis report format Lynis actually writes ([67c433b](https://github.com/jose-guilherme93/sync-win/commit/67c433b0a8206214dc54c4b539c9c39cc86e608f))
* **agent:** refresh the systemd unit during auto-update and ship 0.6.2 ([43b14b3](https://github.com/jose-guilherme93/sync-win/commit/43b14b30e1e014e1067cbae7c72aa89ff49c3d9f))
* **agent:** render unit placeholders so the updater can repair units ([40686c5](https://github.com/jose-guilherme93/sync-win/commit/40686c58eaf5b29353eefa6de1270268d3095377))
* **agent:** report apt package versions ([f3f5aba](https://github.com/jose-guilherme93/sync-win/commit/f3f5abac14983d9dc6c8e4ec11aa7ab9147e4ad1))
* **agent:** report each listening socket once ([c489c4b](https://github.com/jose-guilherme93/sync-win/commit/c489c4be32a9e0a4bf42fb8ce5ffffe41e8f5628))
* **agent:** report journal read failures and strip the source colon ([5a5b205](https://github.com/jose-guilherme93/sync-win/commit/5a5b205910d3050e16232a72c295bf2553cfb415))
* **agent:** report why logs are missing, and make the installer verify it ([950afeb](https://github.com/jose-guilherme93/sync-win/commit/950afeb454fc291282621c6aef4cb8419f1989a4))
* **agent:** report wireless radios without the legacy kernel stats ([bdd63ab](https://github.com/jose-guilherme93/sync-win/commit/bdd63ab2ceb6f1907f94f276aac47a901670cc25))
* **agent:** ship 0.7.2 so the collector fixes reach installed devices ([a75e0c7](https://github.com/jose-guilherme93/sync-win/commit/a75e0c7913ffc0fbc0fde08483b07954253b9a29))
* **agent:** stop logging routine cycle state every ten seconds ([7a5a2b2](https://github.com/jose-guilherme93/sync-win/commit/7a5a2b27fd5089ef60ef79a84d8cd6ec46d653b6))
* **build:** build with Go 1.27 and Alpine 3.24 to clear HIGH CVEs ([181bee6](https://github.com/jose-guilherme93/sync-win/commit/181bee65e31f3af37020075dc689831b68185a48))
* **build:** derive the agent version from the image, not a stale env default ([2de7fb3](https://github.com/jose-guilherme93/sync-win/commit/2de7fb38fc5e72e238ce7d32a7f2675d3ceed3b9))
* **ci:** bound the browser suite so a hang cannot burn the job timeout ([a285c13](https://github.com/jose-guilherme93/sync-win/commit/a285c13583c831dcdb13736942f17bcc03369aea))
* **ci:** build the release for amd64 only ([ab88c05](https://github.com/jose-guilherme93/sync-win/commit/ab88c05fb053347ccb12478e188d4c13ce18cd4f))
* **ci:** clear lint findings and follow the new toolbar in e2e ([515e660](https://github.com/jose-guilherme93/sync-win/commit/515e6609ddc23d51c14457ce697045352959792e))
* **ci:** clear the two failures the merge surfaced on develop ([29e436b](https://github.com/jose-guilherme93/sync-win/commit/29e436bc89aa37507afb3fbacab9f22b734472f2))
* **ci:** context-aware retention deletes and test shadowing ([0b9ba3a](https://github.com/jose-guilherme93/sync-win/commit/0b9ba3afe04e49e54f658cffc7160d129faba5a2))
* **ci:** context-aware store calls and case-insensitive e2e check ([f3e45a1](https://github.com/jose-guilherme93/sync-win/commit/f3e45a148f8362a1c00b15d56b8ae6d01da54a67))
* **ci:** give the browser job room for a slow package mirror ([bc57799](https://github.com/jose-guilherme93/sync-win/commit/bc577995c74e03731deccc2a3fc32a88fb096e2f))
* **ci:** give the browser job room for a slow package mirror ([7e51d96](https://github.com/jose-guilherme93/sync-win/commit/7e51d968dc67ffa4d58e3526b4c9ca686b53c3f9))
* **ci:** make Go lint and Docker healthcheck pass with Go 1.25 ([b0cfd47](https://github.com/jose-guilherme93/sync-win/commit/b0cfd47b8bec3ebaa8538c2e0b999d425dccf39b))
* **ci:** publish a single-arch image before tagging ([4a3a1a2](https://github.com/jose-guilherme93/sync-win/commit/4a3a1a2a4d6a873bdaf1a02a8625586d9dc3f1bd))
* **ci:** publish the server image to Docker Hub ([30dba01](https://github.com/jose-guilherme93/sync-win/commit/30dba01112f1f65a34f9e4f667d8b3143a5177a3))
* **ci:** supply the token in the compose check and follow the new Live behaviour ([866e223](https://github.com/jose-guilherme93/sync-win/commit/866e223bc03c58756b8e35db638d85a997cfb049))
* clear golangci findings in the process collector ([f5adc56](https://github.com/jose-guilherme93/sync-win/commit/f5adc56c21a722a3f24ccb32ea259aa7ad5ea78b))
* commit the contract loader, gofmt and noctx fixes CI caught ([a4fabe0](https://github.com/jose-guilherme93/sync-win/commit/a4fabe01bcf2328ab68931c9e35c0b67ba965136))
* **docker:** open container details in a large side panel ([928aded](https://github.com/jose-guilherme93/sync-win/commit/928aded557c2e9344d0a06fb12cea67a3ecd487b))
* **docker:** run unprivileged and stop pinning the build architecture ([ef3e5cb](https://github.com/jose-guilherme93/sync-win/commit/ef3e5cbaf6fb58688d969064e171d66070b8e935))
* **docker:** split container details into focused tabs ([c488031](https://github.com/jose-guilherme93/sync-win/commit/c488031ab2b13eb72a3fa4590834876cb9951846))
* **e2e:** follow the chart legend after the vitals row was removed ([8297e79](https://github.com/jose-guilherme93/sync-win/commit/8297e7904855cc5b306dc79f3f163a263a8669e9))
* **e2e:** update the Services assertions the screens rework invalidated ([fbd883d](https://github.com/jose-guilherme93/sync-win/commit/fbd883da5d8ed466ef0bb7e0da51122f71fa5ad0))
* gofmt the process store and handler after the merge ([3238bbe](https://github.com/jose-guilherme93/sync-win/commit/3238bbeb3d08abe9d28e44c92921b807677117f5))
* harden SyncWin process mutation guard and cross-lane contract ([bc47da7](https://github.com/jose-guilherme93/sync-win/commit/bc47da7add2ecc6d78282aff557a27d14679fa1f))
* **installer:** download the unit templates and stop requiring a docker group ([ffd8013](https://github.com/jose-guilherme93/sync-win/commit/ffd801332f72080ff6e31785a6e780e21e61d715))
* **installer:** initialize SYNCWIN_INSTALL_LYNIS so set -u does not abort ([0b107ce](https://github.com/jose-guilherme93/sync-win/commit/0b107ce1af50d286a015efabecf8bf69f19a5d32))
* **installer:** stop rolling back a successful install, and keep optional paths optional ([a708a80](https://github.com/jose-guilherme93/sync-win/commit/a708a80f0272baa64777393302b21a1bd900dae3))
* keep device modal tab and return empty file list ([2554836](https://github.com/jose-guilherme93/sync-win/commit/25548360f3395f413690036229e32d6af898c5c1))
* lint, unprivileged image, and publish the release before tagging ([91636f0](https://github.com/jose-guilherme93/sync-win/commit/91636f0d02758b8767633efc98b158663b75c558))
* **lint:** satisfy the checks the CI runs on the new network code ([cc4002f](https://github.com/jose-guilherme93/sync-win/commit/cc4002ffeb8cf9013b4bdccea8d4086c1b14cd6b))
* **lint:** satisfy the new-issues gate ([73a3182](https://github.com/jose-guilherme93/sync-win/commit/73a3182575730a164d1c5cf3f702c3c1f581c817))
* **logs:** marshal the streamed level as a string ([71a69ce](https://github.com/jose-guilherme93/sync-win/commit/71a69ce33ae2a4ad00c0b42d776d1356b79aa669))
* **logs:** show application logs to every operator ([c023c9e](https://github.com/jose-guilherme93/sync-win/commit/c023c9e70b0f9732629f2aa83dd1bc032f2c105a))
* **notifications:** interrupt only for what needs attention ([04336a3](https://github.com/jose-guilherme93/sync-win/commit/04336a3e791c3e6554070b2e5fb35d210e330a2c))
* **notifications:** only interrupt for what needs attention ([765e6a4](https://github.com/jose-guilherme93/sync-win/commit/765e6a4a439441227f6a9e13b9316deb01eb5ef0))
* **notifications:** the recovery notice is never swallowed ([4174ae3](https://github.com/jose-guilherme93/sync-win/commit/4174ae3e99de0d7a4debe4dfa85e8cc41a952d58))
* **notify:** reject non-public addresses in the webhook SSRF filter ([df828b7](https://github.com/jose-guilherme93/sync-win/commit/df828b75d8ae260a65072929556a8515ffdafbe7))
* **overview:** drop the duplicate vitals row ([b16f1b4](https://github.com/jose-guilherme93/sync-win/commit/b16f1b4ebef99d6cc484f835d142f852cf76c0d3))
* **overview:** give the charts more height ([23c9d56](https://github.com/jose-guilherme93/sync-win/commit/23c9d56ab94237718527f2d8ddd50e32379fee36))
* per-core CPU share, full pid, unit refresh and disabled actions ([b37df9f](https://github.com/jose-guilherme93/sync-win/commit/b37df9fa77cbb9a9c4d031f3337b6512685ae714))
* **processes:** per-core CPU share, full pid, honest disabled actions ([763ec28](https://github.com/jose-guilherme93/sync-win/commit/763ec2849e3915b3a82f2c9132508202bb44c4c1))
* **release:** sign the agent in every published image ([709b854](https://github.com/jose-guilherme93/sync-win/commit/709b8549198036d0af199baf4d839dd89c75446b))
* **release:** sign the agent in every published image ([6eaaa15](https://github.com/jose-guilherme93/sync-win/commit/6eaaa15eb613837938fcf5e5a68b6d3d18708e43))
* **remote-access:** accept the browser origin behind the dev proxy ([5b2a609](https://github.com/jose-guilherme93/sync-win/commit/5b2a609ac248b84b572fc63d168e332c217f0abf))
* **remote-access:** compact the terminal card and drop the connecting line ([62f1ca9](https://github.com/jose-guilherme93/sync-win/commit/62f1ca99dc92fa40c204cceda3399fd51b9bf351))
* **remote-access:** move the session audit beside the terminal and actions to Settings ([7ae9d74](https://github.com/jose-guilherme93/sync-win/commit/7ae9d741e2b5a9764e298a603fba05343f60cd80))
* **remote-access:** pass SYNCWIN_ENABLE_REMOTE_ACCESS through production compose ([663a3a3](https://github.com/jose-guilherme93/sync-win/commit/663a3a377efad21db47fb7a2074a37b9b3b7e08d))
* **remote-access:** pass SYNCWIN_ENABLE_REMOTE_ACCESS through production compose ([78301a1](https://github.com/jose-guilherme93/sync-win/commit/78301a1e192261643b8881f78871aa3106a61f37))
* **remote-access:** pass SYNCWIN_ENABLE_REMOTE_ACCESS through production compose ([fa8d896](https://github.com/jose-guilherme93/sync-win/commit/fa8d8964eb5f5c903f664c473c78b4aa748f0705))
* **remote-access:** size the terminal to the available height ([0c67b23](https://github.com/jose-guilherme93/sync-win/commit/0c67b230f65360f59d18366d371bde44ac1b2eb4))
* **retention:** use the documented JSON names for telemetry retention ([013b5c8](https://github.com/jose-guilherme93/sync-win/commit/013b5c80122b4595cd4bf94928cf48bffea8dcec))
* **server:** accept a full-size Lynis report as a command result ([c295836](https://github.com/jose-guilherme93/sync-win/commit/c29583661939784bb7f287be883898a776492bb1))
* **server:** clear new golangci-lint findings ([de82541](https://github.com/jose-guilherme93/sync-win/commit/de82541c8475ec4b89287ce3b8fe05daa53698fd))
* **server:** clear the 13 lint issues blocking CI ([afa62d4](https://github.com/jose-guilherme93/sync-win/commit/afa62d4948817d36d6397c5f0ce7ec9b64a72283))
* **server:** persist device log history instead of losing it each cycle ([bc70d31](https://github.com/jose-guilherme93/sync-win/commit/bc70d3151a3b8ee6ed38df1a7ea515734b0a79bf))
* **server:** pin the validated webhook IP to close DNS-rebinding SSRF ([7cc1cf9](https://github.com/jose-guilherme93/sync-win/commit/7cc1cf99f4d6993ed6a03d739e4bcca8e1a70c75))
* **server:** prevent SSE broadcaster panic and logger request race ([21567b3](https://github.com/jose-guilherme93/sync-win/commit/21567b3e83d6564ac6d9ae2c80e6bbdd75099a34))
* **server:** sanitize installer URLs and harden device attachments ([22c2c14](https://github.com/jose-guilherme93/sync-win/commit/22c2c14dd64275f82cf0480b7aebeb89b12ba477))
* **server:** serve the unit templates the installer needs ([3e237bf](https://github.com/jose-guilherme93/sync-win/commit/3e237bfb96c3ce62b20f0799ba7a5200773309e4))
* **server:** transactional batches, telemetry bounds, safer auth and shutdown ([aee240f](https://github.com/jose-guilherme93/sync-win/commit/aee240f6e6b5e52f8b7fb463794de9f47f6441aa))
* **store:** make telemetry retention indexable and bound the pool ([cbead31](https://github.com/jose-guilherme93/sync-win/commit/cbead31e7863ff05d5e4f4785935cd743af2bcd4))
* **store:** tolerate NULL text columns in device reads ([c5e97b0](https://github.com/jose-guilherme93/sync-win/commit/c5e97b07a68471decb862d326dae877da7a9ff68))
* **store:** use context-aware queries in the alert ack store ([c62d1ce](https://github.com/jose-guilherme93/sync-win/commit/c62d1ce73c9bf067007c70a0cf7cbd4984abc649))
* tolerate NULL device columns, harden webhook SSRF, and re-audit P0 ([8f24d0f](https://github.com/jose-guilherme93/sync-win/commit/8f24d0f7f71f1350166a765899054c111189c00e))
* **topology:** keep each network clear of the device label ([b06d535](https://github.com/jose-guilherme93/sync-win/commit/b06d53516f53ca39027d06e896df7eab9900eb51))
* **web:** bring the Logs screen onto the shared design ([34bd3ee](https://github.com/jose-guilherme93/sync-win/commit/34bd3eed2a9b6536f3487e41f4ef37714ae22ace))
* **web:** correct zero-timestamp formatting and clipped telemetry charts ([81cf7cc](https://github.com/jose-guilherme93/sync-win/commit/81cf7cc5009bd1ba9bd1d0bf1fcce750058ee849))
* **web:** dedupe notifications, reset account state, recover cache, show ports ([ee0a583](https://github.com/jose-guilherme93/sync-win/commit/ee0a583f552946c22f2f6d09bf6d54a8d2aa3fa3))
* **web:** draw the chart skeleton as an area, not a bar chart ([1f3927d](https://github.com/jose-guilherme93/sync-win/commit/1f3927d68a1a1c2d4506a32ea62fdf336ad2e897))
* **web:** draw the chart skeleton as an area, not a bar chart ([ffcdd61](https://github.com/jose-guilherme93/sync-win/commit/ffcdd613b4d2e7b4ff985137279ea2198dfff364))
* **web:** keep the security posture across polls, and repair derived-count rendering ([6a22fc6](https://github.com/jose-guilherme93/sync-win/commit/6a22fc63cfc477c8479331b30e94d4ea0f7f242b))
* **web:** keep the selected device live and separate heartbeat from the chart range ([5b11c18](https://github.com/jose-guilherme93/sync-win/commit/5b11c186dc386fd792d41a641467e41aa718aa2a))
* **web:** make the Packages screen read as a list, not a blank column ([1c919b9](https://github.com/jose-guilherme93/sync-win/commit/1c919b9e19dd8963a07fdf9112bd6ec6d99ba4d1))
* **web:** Packages rendered nothing because nothing fetched the inventory ([89fa669](https://github.com/jose-guilherme93/sync-win/commit/89fa6695e81816a9856e14f0260116ff4c05d635))
* **web:** plot telemetry charts on a real time axis ([647ae63](https://github.com/jose-guilherme93/sync-win/commit/647ae634605c272356f0cce0f888b44ce6a480a7))
* **web:** plot telemetry charts on a real time axis ([8b38edf](https://github.com/jose-guilherme93/sync-win/commit/8b38edf1e8855756ccc3d7572a42135cb8dbbd30))
* **web:** render device history and sparkline charts ([7c65aaf](https://github.com/jose-guilherme93/sync-win/commit/7c65aaf251e23b8d1ed6bdc840969a5913c95535))
* **web:** reset live series when the selected device changes ([1612b3f](https://github.com/jose-guilherme93/sync-win/commit/1612b3fcdf796744ee6db60ec86580115230ff57))
* **web:** resolve type errors and accessibility warnings ([89c70ee](https://github.com/jose-guilherme93/sync-win/commit/89c70ee93b5a33a904915a7f49f2d8dfed5e4045))
* **web:** size the fleet usage bars to the label font height ([fd244f0](https://github.com/jose-guilherme93/sync-win/commit/fd244f07b2496c61493bb3ef7de66b9d14dc5ae9))
* **web:** square the fleet usage bars and mute their fills ([4f5ce79](https://github.com/jose-guilherme93/sync-win/commit/4f5ce790d549226120444c293692692c94074078))
* **web:** stop a repeated socket from blanking the inventory screens ([dfd6516](https://github.com/jose-guilherme93/sync-win/commit/dfd651695a3743b709b7c722d35207fb5df0c525))
* **web:** stop fetch/timer leaks and use same-origin API base ([b972a80](https://github.com/jose-guilherme93/sync-win/commit/b972a80e330cc401c57b5fb83b92de545e9ec185))
* **web:** style every scrollbar for the dark theme ([b43f13f](https://github.com/jose-guilherme93/sync-win/commit/b43f13f51b35c302b4b23506108a7b8170b0f1fe))
* **web:** the account and notification menus could not open ([eb7868b](https://github.com/jose-guilherme93/sync-win/commit/eb7868b33c9b095a1a2fe5c94a14209a42d76905))
* **web:** the account and notification menus could not open ([7305545](https://github.com/jose-guilherme93/sync-win/commit/730554587446588aec7485199c0ac1cbe39c2232))
* **web:** tidy the collapsed sidebar rail ([13f4119](https://github.com/jose-guilherme93/sync-win/commit/13f411978fd652737da1bc125e12bf015bbcab28))


### Performance Improvements

* **agent:** sample the Docker container list once a minute ([5488fec](https://github.com/jose-guilherme93/sync-win/commit/5488fecbb434cefac160f27db7593c3d66b2a8f3))
* **server:** keep the Docker container list out of telemetry rows ([7c05f37](https://github.com/jose-guilherme93/sync-win/commit/7c05f37414806fd11178917ac49ae08ed8ec900e))

## [0.17.0](https://github.com/jose-guilherme93/sync-win/compare/v0.16.1...v0.17.0) (2026-11-11)


### Features

* implement processes tab backend ([88a52fd](https://github.com/jose-guilherme93/sync-win/commit/88a52fde60adc4a28f0e915ab6fc04ff81915bb9))
* implement Processes tab web UI (variant A + tree toggle) ([5f3fd04](https://github.com/jose-guilherme93/sync-win/commit/5f3fd04db4298e26e0c03816ec4f0f26edd51cc5))
* **network:** let the operator arrange the panels ([b673d72](https://github.com/jose-guilherme93/sync-win/commit/b673d72c76ab6a93e5391f4080ccc0fe13d0afc4))
* **network:** let the operator arrange the panels ([1a0d465](https://github.com/jose-guilherme93/sync-win/commit/1a0d4656812888d1a9e86a60bc5381c736fde8fc))
* **network:** open a stat tile into a live inspector ([28eaf89](https://github.com/jose-guilherme93/sync-win/commit/28eaf89d81778cac2d6ccc712b72d419fefa6485))
* process diagnostics and a network inspector from feat/terminal-tabs ([c681119](https://github.com/jose-guilherme93/sync-win/commit/c681119e88c47ead99759d9bb8b1b9900a2b2a09))
* Processes tab — live table, tree, kill actions, history ([9741c7e](https://github.com/jose-guilherme93/sync-win/commit/9741c7ed3f5801437677cb92099e42e256a5d6b6))
* **processes:** diagnose the machine with states, identity and detail ([e276fba](https://github.com/jose-guilherme93/sync-win/commit/e276fbafd50dcdb059429ea9a6841fcf8443372b))
* **processes:** live table, guarded signals, process history and IO ([a1f6ca5](https://github.com/jose-guilherme93/sync-win/commit/a1f6ca5637ab206bf6a1709e27bcd4a975a44c9d))


### Bug Fixes

* **agent:** collector edge cases and updater timeouts ([a70c78f](https://github.com/jose-guilherme93/sync-win/commit/a70c78fb56a2a30b2e92e544566e990510079d9d))
* **ci:** clear lint findings and follow the new toolbar in e2e ([515e660](https://github.com/jose-guilherme93/sync-win/commit/515e6609ddc23d51c14457ce697045352959792e))
* **ci:** context-aware retention deletes and test shadowing ([0b9ba3a](https://github.com/jose-guilherme93/sync-win/commit/0b9ba3afe04e49e54f658cffc7160d129faba5a2))
* **ci:** context-aware store calls and case-insensitive e2e check ([f3e45a1](https://github.com/jose-guilherme93/sync-win/commit/f3e45a148f8362a1c00b15d56b8ae6d01da54a67))
* clear golangci findings in the process collector ([f5adc56](https://github.com/jose-guilherme93/sync-win/commit/f5adc56c21a722a3f24ccb32ea259aa7ad5ea78b))
* gofmt the process store and handler after the merge ([3238bbe](https://github.com/jose-guilherme93/sync-win/commit/3238bbeb3d08abe9d28e44c92921b807677117f5))
* harden SyncWin process mutation guard and cross-lane contract ([bc47da7](https://github.com/jose-guilherme93/sync-win/commit/bc47da7add2ecc6d78282aff557a27d14679fa1f))
* **web:** draw the chart skeleton as an area, not a bar chart ([1f3927d](https://github.com/jose-guilherme93/sync-win/commit/1f3927d68a1a1c2d4506a32ea62fdf336ad2e897))
* **web:** draw the chart skeleton as an area, not a bar chart ([ffcdd61](https://github.com/jose-guilherme93/sync-win/commit/ffcdd613b4d2e7b4ff985137279ea2198dfff364))

## [0.16.1](https://github.com/jose-guilherme93/sync-win/compare/v0.16.0...v0.16.1) (2026-10-31)


### Bug Fixes

* **web:** the account and notification menus could not open ([eb7868b](https://github.com/jose-guilherme93/sync-win/commit/eb7868b33c9b095a1a2fe5c94a14209a42d76905))
* **web:** the account and notification menus could not open ([7305545](https://github.com/jose-guilherme93/sync-win/commit/730554587446588aec7485199c0ac1cbe39c2232))

## [Unreleased]

## [0.16.0] - 2026-10-30

### Changed

- **Docker images are manageable from the dashboard.** A new Images tab lists
  every image with size, age and how many containers use it, pulls a reference,
  shows the engine's own inspect payload, and removes an image (forced when a
  container still uses it). Pull and remove need `allow_docker_images` on the
  device; list and inspect only need `allow_docker_read`.
- **The device charts open populated, and loading looks like loading.** The
  live window used to wait for the poll to accumulate samples, so a device with
  weeks of history in SQLite showed "no samples" for the first ten seconds; the
  live window is now seeded from the database and the poll is layered on top.
  While a chart's first fetch is in flight it shows a shimmering placeholder
  instead of the empty message, and the fleet list does the same before the
  first devices answer.
- **Live behaves like a realtime stream, the way Beszel's one-minute chart
  does.** Entering Live starts clean: the first sample appears at the right edge
  and the last minute fills leftward as it arrives, with the axis anchored on the
  newest sample so the line always touches the right edge. Live no longer
  replays stored history or draws every point of the session squeezed into a
  minute-wide axis. Its line is drawn with straight segments and a dot per
  sample, so a change is visible at the sample where it happened instead of
  being smoothed into a hill by the spline used on the wider ranges.
- **Telemetry now lands every two seconds by default.** A one-minute live chart
  at a ten-second cadence had six chances to show a change; the agent can report
  as often as every two seconds, so a new device stores a sample every two
  seconds and the agent adopts that cadence from the device settings. The
  dashboard polls at that cadence only while the live window is open — every
  other range reads stored history and does not need it. Choose 2, 5, 10, 30 or
  60 seconds per device in Settings.
- **The dashboard has no emoji.** Emoji in empty states, the user menu,
  notifications, toasts and close buttons are replaced by the shared stroke icon
  set in the project's mint, so a screen says the same thing in the same visual
  language.
- **A device's agent version is visible, and an outdated one says so.** The
  header shows `agent 0.11.1 · update` when the device is behind the binary the
  server ships. An older agent keeps working; the badge explains why a newer
  panel can come up empty.
- **Wireless radios are reported even when the kernel keeps no legacy stats.**
  The collector read only `/proc/net/wireless`, which modern drivers leave empty,
  so a laptop on Wi-Fi looked like a machine with no radio. The interface scan
  (sysfs) now always reports the radio, with the signal from `iw` when it is
  installed. Agent 0.11.2.
- **A device is a screen of its own.** Home is now the whole fleet: the sidebar
  carries Home, Alerts, Findings, Reports, Notifications and Settings, and never
  a device menu — not even a disabled one. Opening a device hides the sidebar,
  puts its identity and a `Home` back button in the header, and turns every
  section into one horizontally scrollable tab row (hardware · system · access).
  The redundant "Devices" entry is gone, because Home is that list.

- **The dashboard is opened with a token, not an account.** `SYNCWIN_ACCESS_TOKEN`
  (32 characters or more, generated with `openssl rand -hex 24`) is now the only
  credential. It is read from `.env`, compared in constant time against its
  digest, and never written to the database; the sign-in screen is a single
  field. There is no registration, no email, no password and no recovery flow.
- **The sign-in screen is part of the dashboard.** It uses the same tokens as the
  rest of the shell and the topology glow — a mint radial over the dot grid —
  instead of the old blue card that belonged to no other screen.
- A server without a usable token answers `503` naming `SYNCWIN_ACCESS_TOKEN`
  rather than pretending the token was wrong, and agents keep syncing with their
  own device tokens while the dashboard is locked.
- `SYNCWIN_ENABLE_REGISTRATION`, `SYNCWIN_ADMIN_EMAIL`, `SYNCWIN_ADMIN_PASSWORD`
  and `SYNCWIN_DEV_TOKEN` are gone. An existing install keeps its devices: the
  single owner row and the sessions that reference it are reused.
- **The dashboard has a mark of its own.** A favicon, a 180 px app icon and a
  sidebar/login lockup: an outlined node and a filled node joined by a transfer
  path, mint on the dark surface. The mark is one SVG in `currentColor`, so the
  same geometry serves the tab, the app shell and the README without a second
  drawing.

## [0.15.2] - 2026-10-30

### Fixed

- **Release images sign the agent again.** The Docker build layer for the agent
  is cached by BuildKit without the signing secret's content, so the images
  from 0.13.x through 0.15.1 shipped an empty `sync-win-agent.sig`. Every
  updater failed closed at the signature endpoint. The release build now
  invalidates that layer with the commit SHA, requires `AGENT_UPDATE_KEY`, and
  fails instead of publishing an unsigned image.
- **The agent is now 0.11.1.** A device installed from an unsigned image has no
  public key to verify an update, so it needs one manual binary replacement
  (`/api/agent/download` → `/usr/local/bin/sync-win-agent`, then restart the
  service). After that, auto-update works normally. A fresh install from 0.15.2
  is signed and updates without intervention.

## [0.15.1] - 2026-10-30

### Fixed

- **The topology agent now reaches devices that were already enrolled.** The
  0.15.0 image contained the new Tailscale collector but still advertised agent
  `0.10.0`, so the updater compared `0.10.0` with `0.10.0` and never downloaded
  it. The agent is now `0.11.0` and the collection contract `1.9.0`; a device on
  `0.10.0` updates on its next check and can read the tailnet. A fresh install
  from the 0.15.0 image was unaffected.

## [0.15.0] - 2026-10-30

### Added

- **A network map for every device.** The new **Topology** screen draws the
  networks a device belongs to — each local segment and each overlay — with the
  nodes the device can see. The device sits in the middle, every network hangs
  off it as a hub, and the members sit around their hub.
- **Tailscale is part of the map.** The agent reads the tailnet from the
  daemon's local API socket (world readable on a default install, no root and no
  `tailscale` binary), so the map shows the tailnet name, every peer, its OS, its
  overlay address, whether the path is direct or relayed, tags, exit-node status
  and when it was last seen. Offline peers stay on the map, greyed out.
- **The local segment comes from the kernel.** ARP and NDP neighbours are grouped
  by interface, deduplicated, drawn with their MAC vendor, and the gateway is
  shown once as the way out to the internet. Another SyncWin device on the same
  segment or in the same tailnet is recognised and can be opened from the map.
- **The map is interactive.** Drag a node to pin it, scroll to zoom, drag the
  background to pan, reset the view, and click any node or network for a detail
  drawer with addresses, state, path, tags and members.
- **Honest about what it cannot see.** A LAN member is only a device this host
  has exchanged traffic with recently, never the result of a scan; WireGuard and
  other root-owned tunnels are named instead of hidden; an unreadable Tailscale
  socket is reported as an error instead of an empty peer list.
- The Network screen links to the map, and the map refreshes whenever it opens
  and once a minute while it stays open.

## [0.14.1] - 2026-10-28

### Fixed

- **Telemetry charts plot time, not samples.** A window with only a few
  samples no longer stretches them across the whole card. Every point sits at
  its real timestamp inside the selected window, so a fresh chart grows from
  the left as data arrives, gaps stay where they happened and a device that
  reported for ten minutes of an hour shows ten minutes of line, not an hour
  of invented slope. The spline overshoot that drew a peak at the start of a
  series is gone as well.
- **Every plot shares one look.** The Overview, the Network traffic chart and
  the detailed System tab now draw filled areas with Beszel's chart palette
  (blue CPU, green memory, orange temperature and disk writes, pink upload),
  round time ticks, horizontal-only grid lines and one dark tooltip.

## [0.14.0] - 2026-10-22

### Added

- **The Network screen answers the three questions an operator actually asks.**
  It now shows the traffic of each link over one hour, 24 hours or seven days
  (read from the server, not from a ten-minute memory buffer), the health of
  the stack, the IP configuration, the neighbours on the segment and what is
  reachable from outside.
- **Per-interface traffic history.** The server keeps one sample per interface
  per minute, rolled up into five minute buckets after two days. The global
  telemetry history only has the summed rate, which cannot say which link was
  slow.
- **Packet level health** from the kernel: the share of retransmitted packets
  with a per-second rate, the socket table by state, and the conntrack usage
  against its limit.
- **The network topology of the device:** the default gateway, the resolvers,
  the routing table, the neighbour table, the radio quality of the wireless
  links and the tunnels.
- **The state of the host firewall** (ufw, firewalld, nftables), so a device
  with an exposed port can be checked without leaving the dashboard.
- **The container links are visible now.** They used to be dropped at the
  source, which hid the bridge that was carrying the traffic. They are
  reported and clearly marked as outside the totals, so the fleet numbers do
  not change.

### Changed

- **Every interface carries more than counters:** its kind (physical, wireless,
  tunnel, bridge, container, loopback), the driver behind it, the duplex, the
  carrier changes and the hardware level counters that separate a congested
  network from a broken cable.
- **The notifications only interrupt when there is something to act on.** A
  device that comes back online is recorded in the inbox and in the bell, but
  it no longer raises a toast: the alert stack was filling with good news.
  Only the events the catalogue marks as needing attention interrupt, and only
  while the dashboard is open — the history the dashboard reads when it opens
  is never replayed as a pile of popups.
- **The alerts lead somewhere.** Clicking a toast or a bell row opens the
  device the event is about, the stack moved to the bottom right so it stops
  covering the device readings, three alerts are shown at a time with a link
  to the rest, and the history is grouped by day.
- The device agent is now version **0.10.0** and the collection contract
  **1.8.0**. A device on an older agent keeps working: it reports fewer
  fields, and the screen says what is missing instead of showing zeroes as
  facts.

### Fixed

- **A device that came back was left invisible.** The anti-flap window
  dropped the "back online" notice when another one had been recorded a few
  minutes earlier, so the alert stayed open in the history and the device
  that recovered was never reported. The recovery notice is now recognised as
  the resolution of an alert and is never swallowed.

## [0.13.2] - 2026-10-21

### Fixed

- **Main cannot be released with a red CI.** The Release workflow now starts
  from a successful CI run on the merge commit instead of the push itself, so a
  failing test blocks the image and the tag. `main` is protected: changes go
  through a pull request with every CI check required, administrators included.

## [0.13.1] - 2026-10-21

### Fixed

- **The browser suite follows the chart-first Overview.** Its live-data checks
  still read the vitals cards that 0.13.0 removed, so the end-to-end job failed
  after the release; the checks now assert the Performance chart's CPU series
  and keep the liveness regression on the topbar and fleet row.

## [0.13.0] - 2026-10-20

### Added

- **The Processes tab can diagnose the machine, not just rank it.** A full
  state tally (total, running, sleeping, disk-sleep, stopped, zombie, threads)
  sits above a sortable, searchable table of the reported top consumers plus
  every stopped and zombie row. Rows carry PID/PPID, user, command, state,
  CPU share, RSS and virtual memory, threads and age; selecting one opens the
  full command and an explanation of its state. The agent now reads one
  `/proc/<pid>/stat` per process (cheaper than the old comm+status+stat pass)
  and enriches only the rows it sends, with caps declared in the contract.
- **The device Overview is a full dashboard.** The Performance chart
  plots CPU, memory and temperature with toggles, a percentage axis, a right
  temperature axis, time ticks, hover values, and a legend with each series'
  last/average/min/max; Network and Disk I/O get their own charts with the same
  numbers. Load and cores, storage, interfaces, top processes and system facts
  close the page. Disk I/O history is honest about being live-only: the
  downsampled ranges do not aggregate it.
- **Development sign-in without the login screen.** `make dev` bootstraps
  `demo@sync-win.local` / `sync-win-demo-password` and a fixed
  `SYNCWIN_DEV_TOKEN`; the dashboard trades the token for a normal session
  cookie, so the sign-in form is skipped. The server ignores the token unless
  `SYNCWIN_ENV=development` and it never creates an account.
- **A tab per terminal session in Remote access.** Up to six SSH sessions can
  be open at once, each in its own tab with its own account, ticket and
  connection. The tab strip has a `+` control, each tab is named after the
  program running in it (btop, vim, ...) and falls back to the account name, and
  closing one tab ends only that session. Switching tabs keeps every session
  alive, and a refused session reports why it never connected — including the
  device already serving its six — inside its own tab instead of replacing the
  screen.

### Fixed

- **Scrollbars match the dark theme.** Every scroll container — pages, panels,
  drawers, log viewers — uses a thin, rounded, low-contrast thumb instead of the
  browser's light bar. The terminal's own xterm slider uses the same muted range.
- **Remote access works when the dev dashboard is opened by IP or a Tailscale
  name.** The terminal WebSocket only accepted the Vite proxy's rewritten Host
  and the localhost CORS origins, so a LAN browser got "The session connection
  failed". Development now also accepts the Origin the browser reports;
  production keeps the explicit Host/CORS check.

### Changed

- **The fleet landing is a Termix-style host list.** Each device row shows CPU,
  RAM and disk as inline percentage bars, with status, OS, last-seen and the
  alert chip on the same line. The KPI wall, the per-row sparklines and the
  recent-alerts column are gone; the alert count in the summary links to
  Alerts, and detailed charts remain on the device Overview.
- **The Remote access screen is organized around the terminal.** The session
  audit moved into a scrollable side panel that matches the terminal's height,
  so it can no longer grow the page, and the fixed device actions (restart
  agent, package updates, reboot) moved to Device settings → Maintenance.
- **The full test device supports real Remote access sessions.** The Ubuntu
  container now ships sshd, a `devuser` account and the remote-access helper,
  and its local policy allows that account, so `make dev-agents-full` is enough
  to open live terminals. The image is rebuilt on every run; a stale image had
  silently skipped a Dockerfile change.
- **The browser suite no longer creates an account per run.** It signs in as
  the fixed development account, reuses one device record, and deletes it on
  exit, so running `make test-e2e` leaves the dev fleet unchanged.

## [0.12.0] - 2026-10-19

### Changed

- **Docker screen rebuilt** to match the rest of the dashboard: tiles for
  running, stopped, paused and images, and a searchable container list with
  state filters. Clicking a container opens a large side panel that slides in
  from the right with focused tabs: Overview, Stats (live CPU, memory, network
  and disk), Logs, Terminal and Config. Compose and cleanup are top-level tabs
  instead of hidden sections.
- **The container list is live.** The screen subscribes to the server's Docker
  state stream, and Refresh asks the device for a fresh list instead of
  re-reading a snapshot that can be a minute old.
- **Container actions answer in about two seconds.** The agent drains the Docker
  request queue on its own two-second poller rather than waiting for the
  telemetry cycle, and a failed command states the device's reason.
- **Pause/unpause and kill** join the existing start/stop/restart/remove actions.
  Paused containers now show a Resume action instead of an unusable Start.
- **Container overview and configuration** in the side panel: live metrics,
  health, ports, networks and mounts on Overview; platform, restart policy,
  limits, entrypoint/command and labels on Config. Environment variables are
  never sent, and credential-looking command arguments and labels are masked.
- Paused containers are shown as needing attention on the device instead of
  offering start/restart buttons that Docker would reject.

### Fixed

- Published container ports now appear. The agent decoded the Engine's
  `PrivatePort`/`PublicPort` keys into underscore-tagged fields, so every port
  arrived as zero and the screen showed "no ports".
- A compose scan now reports "no compose project found" when the device simply
  has none, instead of an error caused by a `null` reply or a duplicate empty
  state next to a real error.
- Disabled Docker mutations now answer 403 with "docker mutations are disabled on this server"
  instead of a masked 503 "internal server error".
- A completed container list now refreshes the cached snapshot, so the screen
  that requested it, the next screen that opens, and every live subscriber see
  the same state after an action.

## [0.11.0] - 2026-10-18

### Added

- **Notification center** under Administration: a full inbox with event and
  device filters, unread-only, mark one or all as read, and a link to the
  device. The bell now shows a preview and opens the center; the old settings
  modal is gone.
- **Per-event in-app preferences.** The in-app channel can be enabled or
  disabled and each event type can be included or excluded from popups. Events
  excluded from popups still appear in the inbox.
- **Sound preferences.** Beep, chime, pulse or silent, with volume and a preview
  button. The choice is stored on the server with the account, so it follows the
  browser and applies to the next notification immediately.
- **Server event catalog** (`GET /api/notifications/events`) with the label,
  description and severity of every event, shared by the in-app, Telegram and
  webhook preferences.
- Mark-all-read uses a server-side flag, so it also covers notifications older
  than the page the browser has loaded.

### Fixed

- Disabling in-app alerts and changing the sound previously had no effect: the
  dispatcher recorded and broadcast every event before provider filtering, and
  the toast read a localStorage value that the settings modal never updated.
- The first notification could be silent because every toast created a new
  `AudioContext` before a user gesture; the app now uses one shared context,
  unlocked on the first interaction.

## [0.10.2] - 2026-10-18

Release-process release. The runtime is the same as 0.10.1.

### Changed

- Every release now has written notes in `CHANGELOG.md`. The release workflow
  publishes that section as the GitHub Release body and refuses to publish an
  image without it.
- Backfilled the notes for 0.10.0 and 0.10.1, which previously contained only a
  Full Changelog link.

### Highlights shipped in 0.10.x

- **Live device screens.** Status and telemetry now follow the device poll on
  every screen. The heartbeat poll is a fixed 10s and no longer depends on the
  chart window, so a 7-day view cannot make a healthy device look stale. Manual
  refresh updates the heavy detail too, and switching devices resets the live
  series.
- **Application logs in Settings.** The log viewer is filled in (it was empty
  because application logs carry no owner), receives new entries over SSE
  instead of polling, and can trace a request by `request_id` or
  `correlation_id`.
- **Log settings at runtime.** Persistence level, console level and retention
  can be changed without a restart and survive a restart.
- **Telemetry retention in Settings.** Raw, 1-minute, 5-minute and 1-hour
  windows are editable on the same screen; the API now uses the documented
  `resolution_*` field names.
- **Persistent alert acknowledgements.** An acknowledgement survives a reload
  and follows the account across browsers. The identity is the rule, not the
  reading, so "CPU 92%" stays acknowledged when the next sample reads 95%.
- **CPU clock and core voltage** on the CPU & Thermal screen when the host
  exposes them.
- **Agent housekeeping.** Lynis availability is logged once per change instead
  of every cycle, routine telemetry is logged once a minute, installer backups
  and LEM-era units/binaries/directories are removed automatically, and a stable
  fingerprint lets the server adopt a machine-less device record instead of
  creating a duplicate.
- **Lower telemetry cost.** The Docker container list is sampled once a minute
  and stored once per device in its own column. A normal sample drops from about
  13 KB to about 3 KB.

## [0.10.1] - 2026-10-18

Patch release. Supersedes 0.10.0 and is the recommended build.

### Fixed

- The agent updater's cleanup commands are bounded by a 10-second context. This
  was the lint failure in the 0.10.0 CI run; it has no user-facing behaviour
  change.

### Included from 0.10.0

- Application logs in Administration → Settings, with filters, request tracing
  and a live SSE tail.
- Runtime log persistence level, console level and retention.
- Telemetry retention in Settings.
- Persistent alert acknowledgements.
- CPU clock and core voltage.
- Live status and telemetry on every device screen.
- Agent log-noise reduction, automatic cleanup of old install remnants, and
  fingerprint adoption.
- Docker container sampling that cuts normal telemetry from about 13 KB to
  about 3 KB.

## [0.10.0] - 2026-10-18

### Added

- **Application logs screen** under Administration → Settings: level, category,
  device, time-window and search filters; request and correlation identifiers;
  and a live Server-Sent Events tail instead of a five-second page reload.
- **Global log settings**: persistence level, console level and retention days,
  applied immediately and persisted across restarts.
- **Telemetry retention in Settings**: raw hours, 1-minute, 5-minute and 1-hour
  retention windows for the account's devices, with the server's validation
  message shown when a value is rejected.
- **Persistent alert acknowledgements** per account, with restore; the alert id
  is the rule (`cpu`, `disk:/`, `net:eth0`, …) rather than the rendered value.
- **CPU clock and core voltage** on the CPU & Thermal screen when cpufreq or a
  labelled hwmon CPU rail is available.
- **Live device screens**: the polled summary is merged into the detail payload,
  so status and telemetry stay current on every screen; the manual refresh also
  refreshes the heavy detail.
- **Agent fingerprint command** and installer integration; the server adopts a
  machine-less device record when the fingerprint matches.
- **Automatic agent housekeeping**: routine journal noise reduced from about 18
  lines per minute to one, installer `.bak`/`.rollback` files removed, and
  LEM-era units, binaries, directories and user cleaned up (the old policy is
  migrated first).
- **Docker container sampling** once a minute, stored in a dedicated column so
  telemetry rows no longer carry the container list.

### Fixed

- The log viewer was empty for every account because application logs carry no
  owner; it is now the fleet-wide operator view, while per-device journal logs
  remain scoped to the device owner.
- The retention API returned `1m_days`/`5m_days`/`1h_days` while the UI and
  documentation used `resolution_*`; saves silently failed validation.
- The log SSE stream emitted the level as a number instead of a string.
- The selected device rendered a frozen detail snapshot, which made the topbar
  report `stale` while Home reported `online`; the chart range also controlled
  the heartbeat poll.
- Switching devices on CPU, Memory, Storage, Network or Sensors mixed the two
  devices' sparkline series.
