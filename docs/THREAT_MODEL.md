# RuleSend Threat Model

Status: threat model for the planned self-hosted alpha, not a security certification or a claim that every control is implemented. [IMPLEMENTATION_CONTRACT.md](IMPLEMENTATION_CONTRACT.md) defines the implementation scope. Check release documentation and tests for verified controls. Agent runtime targets macOS and Linux; Windows is a later phase.

## Product and security objective

RuleSend distributes explicitly published AI instruction packs from a single authoritative Hub to locally approved Agent targets. It also shows evidence of installation. It does not remotely execute commands, discover arbitrary private folders, or prove that an AI application obeys a rule.

AI instruction files are security-sensitive configuration: changes may influence the actions of an assistant with access to local data and tools. A small Markdown file must not be treated as harmless merely because it is text.

The primary objectives are:

- Only an authorized administrator can publish a rule version or manage enrollment.
- An Agent can access only its assigned content and submit only its own observations/proposals.
- An Agent writes only to a target explicitly approved on that device.
- Conflicting or interrupted changes preserve recoverable content and never produce a false success report.
- Existing user content outside the managed block remains intact.
- Credentials and private rule content are not exposed through logs, public examples, or the repository.

## Deployment assumptions

- One administrator and one authoritative Hub. Multi-tenant isolation is out of scope for the alpha.
- The Hub host and the Agent operating system accounts are trusted. The product is not a defense against root/administrator access or a compromised same-user process.
- The Hub can read rule content. This is not end-to-end encryption against the Hub operator.
- Remote connections use verified TLS. A development Hub may use HTTP on loopback only.
- When a TLS reverse proxy is used, the backend is bound to loopback or a separately protected channel. Forwarded headers are not trusted from arbitrary clients.
- Agents connect outbound to the Hub and do not expose a remote administration listener.
- The administrator is responsible for compatible company policy, hosting, retention, backups, and recovery.
- Existing application permission/approval boundaries remain in force. Publishing a rule does not grant new application or operating system privileges.

## Assets

| Asset | Required property |
| --- | --- |
| Published rule packs and version history | Integrity, provenance, recoverability |
| Unpublished drafts and local edits | Confidentiality, conflict preservation |
| Existing target file content | Integrity outside the managed block |
| Administrator and device credentials | Confidentiality, separation, revocability |
| Enrollment tokens | Expiry, one-time use, scope |
| Local target registrations | Integrity; Hub cannot silently expand them |
| Device observations and audit events | Attribution, freshness, bounded storage |
| Local backups and Hub database backups | Confidentiality and usable recovery |
| Product builds and updates | Authentic source and documented verification |

## Trust boundaries and data flows

```text
Untrusted browser content or network input
                 |
       Administrator browser
                 | authenticated same-origin HTTPS
                 v
       Hub API and embedded UI ---- SQLite / protected storage
                 ^
                 | authenticated verified HTTPS
                 |
       Agent under local user account
                 |
       Locally approved target mapping
                 |
       Regular file / managed block and protected backup
```

The browser-to-Hub boundary distinguishes administration from ordinary rendering. The Hub-to-Agent boundary distinguishes authenticated content from authority to access a path. The Agent-to-filesystem boundary requires local path validation even when the Hub request is authenticated. External editors remain independent writers outside the Agent's transaction boundary.

## Roles and capability limits

### Administrator

May create/edit drafts, publish a version, choose existing device assignments, change schedules, request a sync, issue enrollment tokens, and revoke device access. The publish action must be explicit; autosaving a draft must not publish it.

Administration over the network must not create a new local path authorization, execute a shell command, read an arbitrary file, or silently adopt another tool's managed block.

### Enrollment token

Permits one registration within its declared scope before expiry. It is not a reusable administrator credential. Redemption and marking the token used must be atomic. A stolen, unused token is a capability within its remaining scope; the UI must allow revocation and identify the resulting registration.

### Device credential

Permits a registered Agent to fetch its assigned published versions/policies and submit its own observations. It does not permit publishing, administrator actions, arbitrary cross-device reads, or modifying another device's acknowledgements.

Revocation blocks subsequent authenticated requests. It does not erase already downloaded content or cancel a local write that has already been authorized and begun.

### Local operator

Registers a concrete target mapping and may remove it. Local approval is independent of enrollment. The Agent runs without administrative privilege unless the operator has an explicit deployment reason; the product must not request privilege escalation to apply a rule.

## Threats and required mitigations

| Threat | Required controls | Residual risk / validation |
| --- | --- | --- |
| Unauthenticated rule or status access | Authentication on every non-public endpoint; narrowly defined health/static routes | Probe routes and unknown endpoints without credentials |
| Device token used for administrator actions | Separate authorization paths and explicit role/scope checks | Test each admin route with a device credential |
| Enrollment replay or race | Scoped one-use tokens; 1–60 minute expiry, default 15 minutes; hashed storage; atomic redemption | Concurrent redemption must yield at most one registration |
| Credential disclosure | No credentials in URLs/logs; protected local files; hashes at rest where verification allows; narrow error messages | Same-user/root compromise remains out of scope |
| Network interception | TLS certificate and hostname verification; loopback-only plaintext | Operator must correctly configure reverse proxy and public certificate |
| Cross-site request forgery | Same-origin operation; anti-CSRF controls if cookies authorize writes; constrained CORS | Test hostile origin requests and preflight behavior |
| Stored/reflected XSS through Markdown or names | Safe text insertion; no executable raw Markdown HTML; restrictive content policy where supported | Test script tags, event handlers, dangerous links, and crafted names |
| Path traversal or arbitrary file overwrite | API relative paths; local allowlisted mappings; reject absolute paths, traversal, drive/UNC forms, symlinks and special files | Validate macOS/Linux semantics separately; reject Windows runtime until supported |
| Symlink replacement or filesystem race | Revalidate target/parents near access; use safe filesystem APIs; fail closed on unsupported objects | A hostile local writer may race checks; no claim of complete protection against same-user attackers |
| Malformed managed markers | Unique identified block; reject nested/duplicate/unclosed markers and injected delimiter text | Tests must prove outside bytes are preserved and malformed files remain unchanged |
| Two actors edit/publish from the same base | Server-assigned draft revisions with compare-and-swap; immutable accepted releases | Reject stale base and keep submitted content available for manual recovery |
| Interrupted/retried publish or apply | Draft revision CAS, immutable published releases, persistent local apply journal and read-back verification | Alpha has no general operation-ID protocol; publishing identical content intentionally may create a new version |
| Offline Agent uploads old local content or deletes targets | Alpha does not upload local edits or propagate remote deletions; scan errors never imply deletion | Any future deletion/sync feature requires versioned tombstones and an explicit reset/retention protocol |
| Local changes lost during incoming update | Compare against known base, preserve conflicts and backups, recheck before replacement | External editors do not share a lock; TOCTOU remains possible |
| Crash between filesystem and DB changes | Temporary file, durable state transitions, recoverable backup, startup reconciliation | Injection tests around write/replace/record/ack boundaries |
| False installation status | Read-back content hash plus target/version acknowledgement; authenticate reporter; track freshness | A compromised Agent can lie; hash proves an observation, not AI compliance |
| Old acknowledgement shown as current | Match acknowledged target, revision and expected hash; retain observed timestamp | Offline/stale status must remain visibly separate |
| Root disappearance mistaken for mass deletion | Distinguish scan errors, missing root and explicit deletions; pause affected mapping | Test unmount/missing directory/permission failure |
| Resource exhaustion | Request/file size limit (pack 128 KiB; resulting Codex file 32 KiB, other targets 128 KiB), bounded concurrency, retry backoff, endpoint limits, bounded logs/history policy | Authorized users can still consume storage; show capacity errors |
| Hub rollback or backup restore | Document consistent backup/restore; reconcile device revisions before freshness claims | Restoring old data may invalidate recent observations and operation records |
| Malicious rule content changes assistant behavior | Explicit publish permission, diff and target scope, immutable history, rollback, separate drafts | Trusted administrator can publish harmful instructions; product does not semantically certify rules |
| Sensitive data committed to public source | Ignore runtime directories; synthetic examples; pre-publication content review | Secret scanning alone does not identify all private content |
| Compromised dependency or update | Minimal dependencies, license inventory, reproducible release procedure where practical, checksums | Checksums alone do not establish publisher authenticity; signing is a separate control |

## Managed-block integrity rules

1. The local mapping identifies an approved file and a specific pack/block, not a server-provided arbitrary absolute path.
2. One managed block per target is the alpha default. Nested and overlapping mappings are rejected.
3. Initial binding requires explicit local approval and shows the selected target, pack and adapter. The local `agent preview` command displays before/after changes without modifying files, state, or recovery journals. It polls the Hub without uploading local file content or installation receipts. Preview output may contain private outside-block context and should remain local; terminal controls are escaped. Preview is only a snapshot and application revalidates the target. Existing markers from another manager are not silently reused.
4. All bytes outside the defined managed span are preserved. Newline behavior at insertion boundaries is specified and tested.
5. Unsupported encoding or ambiguous marker structure is an error, not an invitation to repair the file by guessing.
6. A local edit inside the block creates drift/conflict, not an automatic publish event.
7. Applying a remote revision must not discard an unacknowledged local edit merely because the Hub has a larger revision number.
8. Content containing the reserved delimiters is rejected or escaped by a fully specified format; string concatenation must not allow the content to create a second block.

## Local concurrency and crash limits

Hub compare-and-swap solves competing Hub commits. It does not provide an atomic compare-and-swap operation against files being edited by unrelated applications.

The Agent must use a same-filesystem temporary file, verify the expected local state before replacement, preserve the previous content in protected backup storage, check the resulting content, and maintain enough persistent operation state to reconcile after a crash. Failed or ambiguous writes do not produce successful installation acknowledgements.

There remains a time-of-check/time-of-use window when another process can modify or replace a target or its parents. Advisory locks cannot force arbitrary editors to cooperate. Atomic rename limits partial-file visibility but is not a universal no-data-loss guarantee, and platform durability semantics differ. Release notes must describe tested behavior without claiming impossible general guarantees.

Backups must be usable, named without path traversal, protected at least as strongly as the source, and excluded from the normal sync inputs. Retention should be explicit. Backups that may be the only copy of an unresolved conflict must not be silently pruned.

## Observation integrity

The UI distinguishes:

- **Published:** the Hub accepted the administrator's version.
- **Requested/pending:** a device has work to perform.
- **Installed and verified:** the Agent read the target and reported the expected version/hash at a recorded time.
- **Stale/offline:** the last observation is historical, not a current statement of device contents.
- **Application loaded:** always unverified in this alpha. A future feature needs separate evidence that a named app/session consumed a named rule version.
- **Behavior checked:** always unverified in this alpha. A future feature needs a named scenario and recorded result.

A successful HTTP transfer, active heartbeat, process start, or source hash alone cannot prove application loading. Application loading cannot prove future behavioral compliance. The alpha does not provide evidence-entry workflows. Any future manual evidence must be labeled as manual rather than automatically verified.

## Web UI and API considerations

- Avoid third-party scripts, fonts, analytics, and externally hosted assets in the default UI.
- Never render untrusted data through executable HTML paths. Escape names, error details, Markdown, and diff text consistently.
- Do not put reusable secrets in query strings or example commands committed to source.
- If browser tokens are used directly, avoid persistent browser storage by default. If cookie sessions are used, apply HttpOnly, appropriate Secure and SameSite flags, expiry, and CSRF protection.
- CORS must not allow arbitrary credentialed origins. Host/origin validation and deployment guidance should account for local web attacks and DNS rebinding.
- Validate content type, lengths, enum values, revision fields, and target identifiers. Reject unrecognized security-sensitive fields rather than interpreting them as commands.
- Error responses should identify recoverable categories without exposing stack traces, SQL, token fragments, rule content, or private host paths.
- Long-lived administrator credentials should not be embedded in static UI resources.

## Logging and audit

Audit events should identify actor/device, pack/target identifiers, action, old/new revision, outcome, and time. They should not contain credential material or full private rule bodies by default. Local absolute paths belong in protected local diagnostics, not public examples or central default logs.

Audit logs on a trusted Hub are useful records, not tamper-proof evidence against the Hub administrator. Rate and retention limits must prevent unbounded failure logs. “Last seen” and a successful installation acknowledgement are distinct events.

## Backup, restore, and deletion

- Back up SQLite using a consistency-preserving method, including required journal state, or stop the Hub cleanly before copying its complete data directory.
- Protect backups as sensitive data: they may contain rules, drafts, credentials/verification material, device metadata, and history.
- Document which Agent state is needed to reconnect safely and how to reset a lost Agent without silently republishing old content.
- After a Hub restore, old observations are historical until revalidated. Divergent revision state must fail safely and surface an explicit reconciliation action.
- Alpha has no remote deletion propagation or tombstones. A future deletion feature must preserve tombstones and consider long-offline devices and forgotten identities before adding compaction.
- Removing a device from the UI revokes access; it does not wipe its disk.
- Pack/target deletion is not part of the alpha protocol. A future deletion feature must distinguish removing an owned block from deleting a whole file containing user content.

## Release checks

Before describing an alpha as usable, record results for:

1. Authentication and role/scope denial tests, enrollment expiry/race/reuse, and revocation.
2. Draft revision CAS, immutable releases, explicit restore-to-draft, and safe publish/apply retry behavior.
3. Path traversal, symlink, special-file, invalid-name, missing-root, and permission failures.
4. Managed block preservation, malformed markers, drift, file size caps, and absence of unintended deletion.
5. Local drift preservation without upload, offline reconnect, concurrent edits/publication, interrupted apply, retry and recovery.
6. Acknowledgement freshness, old acknowledgement rejection, hash mismatch, and truthful UI labels.
7. Markdown/script injection and unsafe-origin requests.
8. Request size limits, oversized files, resource errors, and bounded retries.
9. Hub backup/restore and Agent reset using synthetic data.
10. A clean installation using public instructions without real company material or private credentials.

Document which checks are automated, which are manual, and which platforms were actually exercised. A cross-compilation result is not an operating-system filesystem test. Unimplemented controls remain listed as gaps; they are not silently inferred from this document.

## Explicit non-goals and residual risks

- No confidentiality from a trusted Hub administrator or a compromised Agent user account.
- No automatic judgment that a published rule is safe or compatible with every AI product and organization.
- No perfect protection against arbitrary concurrent local writers.
- No proof of future model behavior or automatic refresh of existing chats.
- No erasure of rules already received by a revoked/offline device.
- No multi-Hub consensus, enterprise tenant isolation, Windows Agent runtime, or built-in high availability in alpha.
- No local-content upload/merge, general file mirroring, deletion propagation, or generalized operation-ID protocol in alpha.
- No promise that checksum-only distribution prevents a compromised publisher from supplying a malicious binary.

New integrations, automatic updaters, multiple administrators, or remote filesystem discovery change these assumptions and require revisiting this model before release.
