# Prototype validation

Recorded 2026-10-08 for `0.1.0-alpha.1`. These are implementation checks, not an independent security audit or a certification of AI application behavior.

| Check | Result and scope |
| --- | --- |
| Automated Go tests | Passed on macOS arm64 with Go 1.27.1, including `go test -race ./...`. |
| Static Go analysis | `go vet ./...` passed. |
| Publishing/authentication | Tests cover sessions and origins, role separation, draft CAS, immutable releases, restore-to-draft, enrollment race/expiry, scoped reads, revocation, and receipt validation. |
| Local file safety/recovery | Tests cover outside-block preservation, local drift, malformed markers, result-size limits, private state, locks, symlinks/special files, parent replacement, backups, file modes, and pre/post/ambiguous crash-journal recovery. |
| Two-Agent delivery | Passed using two local Agents and a loopback Hub with synthetic rule files. Publish, local conflict preservation, pause/manual delivery, restart, and revocation were exercised. These are not two physical computers. |
| Regression checks | A manual sync issued between binding and first poll is retained. A valid aggregate response above 8 MiB is accepted within the bounded protocol limit. Remote plaintext listening and unsafe token files are rejected. |
| Real executable smoke test | Hub initialization/start, embedded HTML/CSS/JS delivery, pack publishing, CLI enrollment/binding/once/status, and a preserved local-edit conflict passed in temporary folders. |
| Build targets | Built macOS arm64/amd64 and Linux arm64/amd64 executables. Only macOS arm64 runtime was exercised locally. |
| UI static checks | JavaScript syntax passed. DOM insertion uses safe text APIs; no external runtime dependencies, browser credential storage, or inline handlers. |
| Prototype UI regression checks | Five deterministic DOM-model cases passed: duplicate saves, edits during an in-flight save, publish/save exclusion, enrollment scope in commands, and stable focus/scroll restoration. These are not browser rendering tests. |
| Prototype launcher | The local start script built and served English assets, retained authentication, published a synthetic pack, and preserved its version and administrator identity through restart. No application targets were changed by startup. |
| Browser visual/interaction checks | Not completed in this environment: browser automation was unavailable and native screen/accessibility permission was pending. Responsive appearance, keyboard behavior, and complete browser/API interaction still need a real-browser check. |
| Physical devices and remote HTTPS | Not deployed or exercised. TLS/security policy is tested in code; an actual remote certificate/proxy deployment remains an operator acceptance check. |
| Service startup templates | Documented examples only. launchd/systemd installation and reboot behavior have not been verified here. |
| Public CI/release | Workflows are included. Their execution and a published binary release are not claimed by this local record. |
| App loading / AI behavior | Unverified. RuleSend reports file observations, not model compliance or automatic existing-chat refresh. |

Public-source review found no copied personal/company rules, real device inventory, runtime databases, or credentials. Dependency license texts are included. The declared module baseline is Go 1.25; the included CI matrix is configured to exercise that version on macOS/Linux when the repository is published.

## Before relying on a deployment

Use a synthetic pack on a test target first. Confirm a publish arrives, edit the managed block locally and confirm conflict preservation, pause delivery, restore a draft from history, and revoke a disposable device. Review the target's actual application loading behavior in a fresh session. Check the browser UI and remote HTTPS deployment in the environment you intend to use. Keep independent backups and avoid multiple automatic writers for the same target.
