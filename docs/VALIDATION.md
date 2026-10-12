# Prototype validation

Core runtime checks recorded 2026-10-08 for `0.1.0-alpha.1`; browser and UI checks updated 2026-10-12. These are implementation checks, not an independent security audit or a certification of AI application behavior.

| Check | Result and scope |
| --- | --- |
| Automated Go tests | Passed on macOS arm64 with Go 1.27.1 and Linux arm64 with Go 1.25.14, including `go test -race -mod=readonly ./...`. |
| Static Go analysis | `go vet ./...` passed on macOS arm64 and Linux arm64. |
| Publishing/authentication | Tests cover sessions and origins, role separation, draft CAS, immutable releases, restore-to-draft, enrollment race/expiry, scoped reads, revocation, and receipt validation. |
| Local file safety/recovery | Tests cover outside-block preservation, local drift, malformed markers, result-size limits, private state, locks, symlinks/special files, parent replacement, backups, file modes, and pre/post/ambiguous crash-journal recovery. |
| Two-Agent delivery | Passed using two local Agents and a loopback Hub with synthetic rule files. Publish, local conflict preservation, pause/manual delivery, restart, and revocation were exercised. These are not two physical computers. |
| Regression checks | A manual sync issued between binding and first poll is retained. A valid aggregate response above 8 MiB is accepted within the bounded protocol limit. Remote plaintext listening and unsafe token files are rejected. |
| Real executable smoke test | Hub initialization/start, embedded HTML/CSS/JS delivery, pack publishing, CLI enrollment/binding/once/status, and a preserved local-edit conflict passed in temporary folders. |
| Local preview | Safety tests cover unchanged content/modes/timestamps/state, pending recovery journals, unsafe paths, invalid releases, scope, and terminal escaping. Executable smoke checks passed for text/JSON/filtering, nonzero conflicts, ready/current/paused states, no extra receipts, and predicted hash matching actual application. |
| Build targets | Built macOS arm64/amd64 and Linux arm64/amd64 executables. macOS arm64 and a Linux arm64 virtual environment were exercised; amd64 builds were not runtime-tested. |
| Linux runtime | Official `golang:1.25-bookworm` image, Go 1.25.14, Linux arm64 under OrbStack (kernel 6.19.13). Full race tests, vet, build, and executable version check passed with an unprivileged user, read-only source, and tmpfs test storage. This is not physical-device or power-loss testing. |
| UI static checks | JavaScript syntax passed. DOM insertion uses safe text APIs; no external runtime dependencies, browser credential storage, or inline handlers. |
| Prototype UI regression checks | Thirteen deterministic DOM-model cases passed, including delayed save/restore edit preservation and write exclusion, clean/dirty policy refresh, policy-save baselines, the login landmark, scoped command copying, clipboard fallback, and focus/scroll restoration. These are separate from the Chrome checks below. |
| Prototype launcher | The local start script built and served English assets, retained authentication, published a synthetic pack, and preserved its version and administrator identity through restart. No application targets were changed by startup. |
| Browser visual/interaction checks | Passed in headless Chrome 154.0.8037.99 on macOS arm64 at 1440×1000, 390×844, and 320×844. Login/rejection, draft/save/publish, delayed restore edits, cross-client policy refresh, enrollment/guide/copy, real Agent preview/apply/receipt, pause, local conflict, revocation, modal Escape, and layout bounds were exercised with synthetic local fixtures. No unexpected browser warnings or JavaScript errors. Screenshots were inspected outside the checkout. Browser plugin was unavailable; existing Playwright 1.62.1 and a separate temporary Chrome profile were used. |
| Browser regression fixes | Reproduced and fixed restoration overwriting concurrent edits and stale policy controls. Added the missing login skip-link target and fixed 320px navigation overflow. |
| Physical devices and remote HTTPS | Not deployed or exercised. TLS/security policy is tested in code; an actual remote certificate/proxy deployment remains an operator acceptance check. |
| Service startup templates | Documented examples only. launchd/systemd installation and reboot behavior have not been verified here. |
| Public CI/release | Workflows are included. Their execution and a published binary release are not claimed by this local record. |
| App loading / AI behavior | Unverified. RuleSend reports file observations, not model compliance or automatic existing-chat refresh. |

Linux image digest: `sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437`. The disposable container mounted no host credentials and published no ports.

Browser tests used an isolated loopback Hub, private temporary credentials, and disposable rule files. They did not bind real application rule paths or alter existing sync jobs. Some refresh/concurrency checks used controlled timers and delayed network responses. Safari, Firefox, physical-device HTTPS deployment, and application rule loading were not exercised.

Public-source review found no copied personal/company rules, real device inventory, runtime databases, or credentials. Dependency license texts are included. The declared module baseline is Go 1.25; the included CI matrix is configured to exercise that version on macOS/Linux when the repository is published.

## Before relying on a deployment

Use a synthetic pack on a test target first. Confirm a publish arrives, edit the managed block locally and confirm conflict preservation, pause delivery, restore a draft from history, and revoke a disposable device. Review the target's actual application loading behavior in a fresh session. Check the browser UI and remote HTTPS deployment in the environment you intend to use. Keep independent backups and avoid multiple automatic writers for the same target.
