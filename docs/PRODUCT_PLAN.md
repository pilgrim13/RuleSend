# Prototype 1 plan

RuleSend helps one person publish shared AI instructions and see which devices installed them. The first prototype focuses on a complete, understandable delivery workflow for Codex, Claude Code, and explicit Markdown targets.

This plan defines scope and acceptance criteria; it is not a completion report. See [validation](VALIDATION.md) for recorded checks, [commands](CLI.md) for current behavior, and the [implementation contract](IMPLEMENTATION_CONTRACT.md) for API details.

## Scope

- One administrator, one self-hosted Hub, multiple Agents; English UI and primary documentation.
- macOS/Linux Agent targets. Source build with Go 1.25 or newer; actual platform validation is tracked separately.
- Markdown rule packs: edit a draft, publish an immutable version, view history, restore an older release to a draft.
- Scoped, expiring, single-use enrollment tokens and device revocation.
- Explicit local bindings to `AGENTS.md`, `CLAUDE.md`, or another Markdown file; one pack per target file.
- Managed-block replacement that preserves unrelated content and stops on local changes or ambiguous markers.
- Automatic intervals from 15 seconds to 24 hours, manual requests, pause, and Sync now.
- Device/target observations showing desired version, installed version, last contact, conflicts, and errors.
- Pack text up to 128 KiB; final Codex files up to 32 KiB and other target files up to 128 KiB, including existing content.

General folder sync, Windows support, local-edit upload, automatic merging, force overwrite, remote deletion, and unbind are outside this prototype. Claude general chat, Projects, and Cowork are not configured automatically.

## Core user flow

1. Start a Hub, sign in locally, and create a small synthetic rule pack.
2. Save the draft and explicitly publish it. Unpublished content must stay on the Hub.
3. Create a scoped enrollment token and register an Agent on a receiving device.
4. On that device, approve a concrete file path and adapter. Binding reports the target without modifying the file; full content preview is not available yet.
5. Apply the published version and read the target back. Show its version/hash receipt in the Hub.
6. Publish a change and observe each target independently. An offline device remains pending or stale until it reports again.
7. Edit a managed block locally and confirm the content is preserved and reported as a conflict. Review and resolve it locally; there is no automatic overwrite path.
8. Restore an older release to a draft and publish it as a new version. Test pause, a manual request, and revocation with disposable targets.

Credentials are entered by the user through the local UI or a private token file, not copied into AI chats or source control. A separate physical device uses the Hub's HTTPS address; loopback addresses refer only to the current machine.

## Architecture and boundaries

```text
Browser → Hub: draft → publish → immutable release
             ↑
             └── Agent outbound requests → approved local managed block
                                        → read-back version/hash receipt
```

- One Go executable provides Hub and Agent commands, an embedded web UI, and SQLite storage.
- The Hub owns rule revisions, policies, enrollment, and observations. Draft revision checks reject stale edits.
- The Agent owns local path approval, scheduling state, backups, and interrupted-write recovery. The Hub cannot grant a new path or run a shell command.
- Remote traffic requires verified HTTPS. Plain HTTP is limited to loopback; the Hub operator can read rule content.
- Before replacement, preserve the old file and a recovery journal; verify the resulting file before reporting installation.
- Publishing, file installation, application loading, and AI behavior are distinct states. Application loading and behavior remain **unverified** in Prototype 1.
- Hub availability and consistent backups are the operator's responsibility. Existing local files remain when the Hub is unavailable.

## Acceptance criteria

| Area | Required outcome |
| --- | --- |
| Authoring | Draft saves do not deploy; stale edits are rejected; published versions remain immutable. |
| Enrollment | Expired, reused, revoked, or out-of-scope credentials cannot perform unauthorized operations. |
| Local approval | No file is modified during binding; application uses only explicitly approved regular-file targets. |
| Preservation | Bytes outside the managed block survive updates; local drift and malformed markers stop application without discarding content. |
| File safety | Unsafe paths, symlinks, unsupported objects, and oversized results are rejected; missing paths do not imply deletion. |
| Delivery | Two Agents receive a published version; pause blocks both scheduled and explicit application; manual requests survive restart. |
| Recovery | Tests cover interrupted writes before/after replacement and ambiguous local changes; recoverable copies remain available. |
| Truthful status | A receipt requires file read-back; heartbeat is separate; stale or older receipts never imply current installation. |
| Browser workflow | Login, edit, publish, enroll, inspect status, change policy, and restore a draft work in a real browser with keyboard access and narrow-screen layouts. |
| Remote use | Two physical devices complete publish, disconnect/reconnect, conflict, and revocation checks through a real HTTPS deployment. |

Independent local editors can race filesystem operations. Backups and checks reduce risk but do not establish universal loss-free concurrent editing or model compliance.

## Recorded checks and remaining gaps

The current validation record reports passing Go tests, race checks, static analysis, an executable smoke test, and two local Agents on macOS arm64. It also records macOS/Linux cross-builds. These results do not establish runtime behavior on every build target.

Real-browser interaction and visual checks, Linux runtime testing, separate physical devices with HTTPS, and installed service/reboot behavior remain unverified in that record. Public CI/release execution must be checked independently; included workflows alone are not evidence of success. App loading and AI behavior remain outside automated verification.

## Next step

Complete the real-browser authoring-to-installation workflow and fix observed usability or correctness issues. Then run the acceptance scenario on two physical devices over HTTPS, include a Linux runtime where available, and update the validation record with exact environments and outcomes. Expand scope only after this delivery loop is demonstrated.
