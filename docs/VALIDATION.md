# Prototype validation

Core runtime checks recorded 2026-10-08 for `0.1.0-alpha.1`; browser, physical-device, and fresh CLI checks updated 2026-10-12. These are implementation checks, not an independent security audit or a certification of AI application behavior.

| Check | Result and scope |
| --- | --- |
| Automated Go tests | The current source-queue implementation passed the full `go test -race -mod=readonly ./...` suite on macOS arm64 with Go 1.27.1. Earlier baseline checks also passed on Linux arm64 with Go 1.25.14; the optional source feature has not been revalidated there. |
| Static Go analysis | `go vet ./...` passed for the current implementation on macOS arm64. The earlier Linux arm64 result precedes the optional source feature. |
| Publishing/authentication | Tests cover sessions and origins, role separation, draft CAS, immutable releases, restore-to-draft, enrollment race/expiry, scoped reads, revocation, and receipt validation. |
| Local file safety/recovery | Tests cover outside-block preservation, local drift, malformed markers, result-size limits, private state, locks, symlinks/special files, parent replacement, backups, file modes, and pre/post/ambiguous crash-journal recovery. |
| Two-Agent delivery | Passed using two local Agents and a loopback Hub with synthetic rule files. Publish, local conflict preservation, pause/manual delivery, restart, and revocation were exercised. These are not two physical computers. |
| Regression checks | A manual sync issued between binding and first poll is retained. A valid aggregate response above 8 MiB is accepted within the bounded protocol limit. Remote plaintext listening and unsafe token files are rejected. |
| Real executable smoke test | Hub initialization/start, embedded HTML/CSS/JS delivery, pack publishing, CLI enrollment/binding/once/status, and a preserved local-edit conflict passed in temporary folders. |
| Local preview | Safety tests cover unchanged content/modes/timestamps/state, pending recovery journals, unsafe paths, invalid releases, scope, and terminal escaping. Executable smoke checks passed for text/JSON/filtering, nonzero conflicts, ready/current/paused states, no extra receipts, and predicted hash matching actual application. |
| Build targets | Built macOS arm64/amd64 and Linux arm64/amd64 executables. macOS arm64 and a Linux arm64 virtual environment were exercised; amd64 builds were not runtime-tested. |
| Linux runtime | Official `golang:1.25-bookworm` image, Go 1.25.14, Linux arm64 under OrbStack (kernel 6.19.13). Full race tests, vet, build, and executable version check passed with an unprivileged user, read-only source, and tmpfs test storage. This is not physical-device or power-loss testing. |
| UI static checks | JavaScript syntax passed. DOM insertion uses safe text APIs; no external runtime dependencies, browser credential storage, or inline handlers. |
| Prototype UI regression checks | Twenty-nine deterministic DOM-model cases passed, covering draft/write exclusion, policy refresh, authentication feedback, scoped command copying, source queue ownership, and AI proposal authoring. Eight AI cases cover scoped prompt copying and fallback, schema/size/Unicode checks, consent, local-only approval, cancellation, stale and concurrent edits, safe text rendering, bounded diff work, and consent withdrawal during approval. These are separate from the Chrome checks below. |
| Git-backed source API | Synthetic Go cases passed for initial proof/ownership anchoring, separate publisher authentication, frozen draft requests with later saves, queue concurrency, idempotent completion, proof conflicts, retryable failure and terminal rejection, retained jobs/identity on restart, repeated identical publication, and rejection of silently omitted source configuration. These remain separate from the live publisher checks below. |
| Git-backed browser and delivery | Passed in headful Chrome on the office Mac using a private browser profile, the existing scoped SOCKS transport, and normal HTTPS certificate verification. Login used Secure, HttpOnly, SameSite=Strict session cookies. Draft edit/save/compare/restore and explicit queued publication were exercised. The final changed-source and restored-source browser checks reported no JavaScript errors. Identical content reached both global Codex targets; a harmless trailing-newline change then produced Git content and manifest releases before Hub publication and two applied read-back receipts. Restoring the original approved bytes produced another source release and two further applied receipts. |
| Private integration fixtures | Seven generic Git-publisher checks, thirteen receipt-observer checks, and nine browser-launcher checks passed in temporary fixtures. These adapters remain deployment-specific and outside the public checkout; they do not establish a bundled general-purpose Git publisher or universal browser/transport support. |
| Source-queue CLI loading | At both the initial identical-content publication and the final restored release, fresh no-model Codex CLI input on both Macs contained the entire canonical body and discovered both required generic skills. Five installed skill files matched the current source manifest. This input check is separate from the earlier synthetic-marker behavior requests. |
| Source-queue service restart | Hub and private publisher process IDs changed while their original credential identity, initial source anchor, source checkout, and current proof were preserved. A protected Hub database backup passed SQLite integrity checking. Both physical Macs then reported fresh applied read-back receipts matching the final restored body and current published version. This was a process restart, not an operating-system reboot. |
| Prototype launcher | The local start script built and served English assets, retained authentication, published a synthetic pack, and preserved its version and administrator identity through restart. No application targets were changed by startup. |
| Browser visual/interaction checks | Passed in headless Chrome 154.0.8037.99 on macOS arm64 at 1440×1000, 390×844, and 320×844. Login/rejection, draft/save/publish, delayed restore edits, cross-client policy refresh, enrollment/guide/copy, real Agent preview/apply/receipt, pause, local conflict, revocation, modal Escape, and layout bounds were exercised with synthetic local fixtures. No unexpected browser warnings or JavaScript errors. Screenshots were inspected outside the checkout. Browser plugin was unavailable; existing Playwright 1.62.1 and a separate temporary Chrome profile were used. |
| Browser regression fixes | Reproduced and fixed restoration overwriting concurrent edits and stale policy controls. Added the missing login skip-link target and fixed 320px navigation overflow. |
| Physical devices and remote HTTPS | Passed on two physical macOS arm64 computers using a private Tailscale HTTPS proxy and normal certificate verification. Both devices installed Codex and Claude rule files, preserved outside text, previewed without writes, and automatically received published updates with four read-back receipts. Pause, Agent stop/reconnect and catch-up, local conflict preservation, and authenticated revocation were exercised. The receiving computer used a temporary loopback SSH SOCKS proxy for its userspace-networking route; this does not establish direct network reachability in every environment. |
| Service startup/restart | macOS LaunchAgents for the Hub and both Agents were installed, started, and restarted. PIDs changed, bindings and file bytes survived, and a subsequent version propagated. Test services were unloaded and removed afterward. Whole-machine reboot, login startup, and Linux systemd installation remain unverified. |
| Controlled global Codex pilot | Passed on both physical Macs after comparing the approved source by content and manifest. Existing hourly writers were unloaded and their exact plists parked with local backups; each global file contains one RuleSend block with the byte-exact canonical body and its original mode. Five generic skill files match their manifest; knowledge vaults and device mappings were excluded. Automatic delivery at a 60-second interval, Hub/Agent/transport process restarts, and a subsequent identical publication produced two fresh read-back receipts. Fresh no-model Codex CLI input contains the entire canonical body and discovers both required skills through verified catalog root aliases. Claude files/settings and broader dotfiles/health jobs were preserved. |
| Public CI/release | Workflows are included. Their execution and a published binary release are not claimed by this local record. |
| App loading / AI behavior | Passed for fresh Codex CLI sessions on both physical Macs: deterministic prompt-input inspection found the deployed marker, and actual default-model requests returned the file-only random marker with tools/integrations disabled. Claude Code requests were blocked by missing authentication on both hosts; its rule files were installed, but application loading remains unverified. Desktop applications, existing conversations, other models, and general compliance remain unverified. RuleSend receipts still report file observations rather than these separate application checks. |

Linux image digest: `sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437`. The disposable container mounted no host credentials and published no ports.

Browser tests used an isolated loopback Hub, private temporary credentials, and disposable rule files. They did not bind real application rule paths or alter existing sync jobs. Some refresh/concurrency checks used controlled timers and delayed network responses. Safari and Firefox were not exercised. The separate physical-device acceptance checks are described below.

The optional source job API was added after the earlier loopback browser and
disposable physical-device checks. It was then exercised through the retained
global Codex pilot: a real authenticated browser queued frozen drafts, and a
separately reviewed private publisher verified and recorded the canonical Git
source before completing each Hub release. Three successive browser requests
covered unchanged content, a harmless trailing-newline change, and restoration
of the original approved body. Each completed release reached both physical
Codex targets with fresh read-back receipts. Source proof, Hub publication, file
installation, and application input were checked separately; no new general AI
behavior claim follows from these publications.

The private integration fixtures do not establish power-loss recovery. An
ambiguous Git state or stale release lock can still require manual review;
automatic lock deletion, history reset, or force overwrite is not implied.
Current Linux execution, other browser engines, operating-system reboot/login
startup, desktop application loading, and Claude loading remain unverified for
this source-backed rollout. Earlier Linux/cross-build results do not establish
execution of the optional feature.

The separate native browser-launcher check was blocked by macOS Accessibility
and Screen Recording permissions. Its fixture results do not establish that
the shortcut opened successfully on the actual desktop. This limitation is
separate from the completed headful browser session and does not imply broader
network configuration or unrelated system permissions were verified.

Physical-device acceptance used the executable from commit `d21a4ba` on macOS 15.7.2 (24G325) and macOS 26.2 (25C56), both arm64. Both installed Codex CLI 0.159.1 sessions resolved their configured default model to `gpt-6-astra`. Claude Code versions were 2.1.105 and 2.1.140; read-only authentication status confirmed neither was signed in. The synthetic marker appeared only in the published instructions, never in the question. Codex also used ephemeral sessions; no unrelated tools or external MCP servers were started for the application probe.

The initial rollout proceeds on the basis of the verified Codex CLI results.
Claude Code loading remains unverified and is deferred until authentication is
available.

The physical tests bound only disposable project `AGENTS.md` and `CLAUDE.md` files. Global rule files and Claude settings remained byte-for-byte unchanged. Existing sync jobs and network services were preserved; the added HTTPS handler was removed and the final Tailscale configuration matched the original exactly. Temporary services and the SSH tunnel were stopped. Private fixture data and recovery state remain outside the checkout; no credentials, real device addresses, personal rules, or company content are included here. Service restart and Agent disconnection are distinct from an operating-system reboot or a network outage.

A later controlled rollout on 2026-10-12 migrated the global Codex targets on
the same two Macs using the previously tested executable. The private canonical
rule body was reused unchanged; only its generic management skill gained delivery
ownership routing, with 113 synthetic tests passing in its separate distribution
repository. A separately reviewed local publishing bridge verified the clean Git
release, manifest hashes, and exact Hub body. The Hub and receiving services are
retained for this pilot, with private per-device rollback backups and ownership
records. Their machine addresses, personal instructions, credentials, and
deployment-specific scripts remain outside this public checkout.

The global loading checks used Codex CLI 0.159.1 and made no model requests.
They establish full instruction input and skill discovery, while the earlier
synthetic-marker requests remain the behavior evidence. The operator’s active conversation
received updated instructions during the rollout, which does not establish
updates to other open conversations. Whole-machine reboot/login startup, other desktop sessions, and
Claude loading remain unverified. Actual rollback of the live global files was
not performed; the preserved backups and reviewed recovery tooling remain
available. Earlier disposable-test cleanup claims refer to those earlier tests,
not this retained pilot.

Public-source review found no copied personal/company rules, real device inventory, runtime databases, or credentials. Dependency license texts are included. The declared module baseline is Go 1.25; the included CI matrix is configured to exercise that version on macOS/Linux when the repository is published.

## Before relying on a deployment

Use a synthetic pack on a test target first. Confirm a publish arrives, edit the managed block locally and confirm conflict preservation, pause delivery, restore a draft from history, and revoke a disposable device. Review the target's actual application loading behavior in a fresh session. Check the browser UI and remote HTTPS deployment in the environment you intend to use. Keep independent backups and avoid multiple automatic writers for the same target.

## AI-assisted authoring verification

The prompt-copy, proposal-import, line comparison, consent, local-draft approval,
explicit save/publication, and stale-response rejection passed in real Chrome on
macOS arm64 using an isolated temporary Hub and synthetic rules. A real Agent
installed the published synthetic content into a disposable Codex target, retained
its outside text, and reported a matching read-back receipt. Review dialogs and
editor layout fit 1440px, 390px, and 320px viewports; screenshots were inspected.
The clipboard implementation was mocked to avoid changing the system clipboard.
No live rules, global application targets, AI providers, or model calls were used.
The Hub test suite and 29 deterministic DOM-model checks passed; the executable
was built with an official checksum-verified Go 1.27.1 toolchain.

This establishes the manual proposal workflow, not the quality or safety of an
AI's recommendations. Existing source-queue regression checks passed; the AI
workflow does not change those APIs. It does not establish that imported rules
were read or followed by an AI, or that arbitrary provider outputs will satisfy
the requested JSON format.
