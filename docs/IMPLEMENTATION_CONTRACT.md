# Prototype 1 implementation contract

RuleSend is an original implementation with no Syncthing source copied: one Go binary, SQLite, and an embedded web app. Public product copy and documentation use English. The README provides an overview, architecture, installation steps, and everyday usage, with detailed contracts in docs/.

## First release scope

AI rule-pack authoring and device deployment, not arbitrary filesystem mirroring. One UTF-8 Markdown document per pack, draft -> explicit publish -> immutable version -> local, explicitly selected app/file targets. Each target contains an owned managed block; bytes outside it are preserved. Existing tools' blocks remain untouched. A locally edited owned block becomes a conflict, never overwritten automatically. Browser editing works anywhere the authenticated Hub is reachable. An agent reports file installation only; app loading and behavior always remain unverified unless explicit evidence is recorded later (not automated in this alpha).

One admin; multiple devices. Codex, Claude Code, custom Markdown targets. Claude Chat/Cowork and dots are not automatically configured. HTTPS required off loopback; API credential secrets never placed in URLs/logs. Agent owns network outbound, Hub cannot assign arbitrary local paths or shell commands.

## AI-assisted authoring

The browser offers an explicit prompt-copy and proposal-import workflow without
provider integration. Only the selected local draft and user request enter the
reviewable prompt. Imported JSON proposals contain a pack ID, draft revision,
exact content SHA-256, complete Markdown, and explanatory lists. These identifiers
detect a changed baseline; they do not authenticate the AI or attest rule safety.
Review shows bounded highlighted line changes and complete documents. Approval
requires consent and a fresh check of the saved Hub draft, changes only the local
draft, and preserves concurrent edits. Save and Publish remain separate explicit
actions under the existing APIs and source ownership. See [AI_AUTHORING.md](AI_AUTHORING.md).

## Shared JSON types in internal/core (root owns)

Pack: id, name, description, content (draft text), draftRevision (int64), publishedVersion (int64), publishedHash, mode (auto/manual), intervalSeconds, paused, trigger (int64), updatedAt (RFC3339 UTC), optional source. Release: packId, version, content, hash, message, createdAt, optional source proof. Device: id, name, platform, packIds, revoked, lastSeen. Receipt: deviceId, packId, targetId, adapter, targetLabel, version, hash, status (applied/conflict/error/pending), error (safe description), checkedAt, appLoaded (always unverified), behaviorVerified (always unverified). Event: id, kind, actor, message, createdAt. Snapshot: packs[], devices[], receipts[], events[], publications[] (metadata without frozen content). Target label user-friendly; do not expose full local paths to server.

Agent poll response: device + plans[] where each plan = pack {id,name,mode,intervalSeconds,paused,trigger} plus release {packId,version,content,hash,message,createdAt} or null. Published content only, no drafts. Agent posts receipts only for authorized pack and valid published version/hash. Applied must match server release hash; no claim of tool compliance.

## HTTP contract (camelCase JSON)

Errors: status 400/401/403/404/409/413/429/500 and {"error":"safe human readable"}; no raw internal exceptions.

Admin auth: POST /api/login {token} -> {ok:true}, HttpOnly SameSite=Strict session cookie. POST /api/logout. GET /api/state -> Snapshot. Unauthenticated /api/state = 401 (UI shows login). All admin mutations require JSON Content-Type and same-origin checks; cookie routes reject missing or foreign Origin. CLI admin functions may use admin Bearer token instead, never agent token. GET /healthz contains version only.

- POST /api/packs {name,description,content} -> Pack (201).
- GET /api/packs/{id} -> {pack:Pack,releases:Release[]}.
- PUT /api/packs/{id}/draft {baseRevision,content} -> Pack (CAS 409 on stale).
- POST /api/packs/{id}/publish {baseRevision,message} -> Release (200) for ordinary packs, or {publication:SourcePublication} (202) for source-managed packs; 409 if stale or a source request is unresolved. Publishing identical content can still create a deliberate immutable release.
- POST /api/packs/{id}/restore {version,baseRevision} -> Pack; copies historic release to draft, does not publish.
- PATCH /api/packs/{id}/policy {mode,intervalSeconds,paused} -> Pack.
- POST /api/packs/{id}/sync-now {} -> Pack (increment trigger, does NOT unpause).
- POST /api/enrollment {packIds:[],expiresMinutes:15} -> {token,expiresAt}. Scoped, single-use, 1..60 minute expiry. Token shown once, hash at rest.
- POST /api/enroll {token,name,platform} -> {deviceId,deviceToken,packIds}. No admin auth. Transactional consume prevents two enrollments. New device token shown once, hash at rest.
- POST /api/devices/{id}/revoke {} -> {ok:true}; revoked token cannot poll/report.
- POST /api/agent/poll {} -> {device:Device,plans:[{pack:Pack,release:Release|null}]} device Bearer only. Heartbeat persists and admin displays age; no fictitious receipts.
- POST /api/agent/receipts {receipts:Receipt[]} -> {ok:true}. Server fills deviceId and checkedAt. Bound maximum count/field lengths. Safe local errors cannot contain credentials/content.

### Optional source publication

`SourceProof` = {commit,releaseId,version,hash}: lowercase 40-character Git commit,
release ID and source version label, and lowercase 64-character SHA-256 of the
exact rule body. Source labels are independent of the Hub's increasing pack
version. `Pack.source` = {label,proof,pending,status,error}; pending is the
unresolved job ID or empty, and status is idle/pending/failed.

`SourcePublication` = {id,packId,hash,message,baseDraftRevision,baseSourceProof,
basePublishedVersion,basePublishedHash,status,error,createdAt,completedAt?,
releaseVersion?,proof?}. Status is pending/failed/completed/rejected. Admin
snapshots include up to 100 recent job records and omit their frozen content.
The dedicated publisher's pending-job response additionally includes exact
`content`, including an empty string for an approved empty draft.

`hub serve --source-config FILE` explicitly anchors existing published packs to
reviewed source proofs. Its initial hash must match published content. Anchors
and outstanding jobs persist across restart; omitted owners and changed anchors
are rejected. The separate private `source-token` remains pinned to its original
identity and cannot be silently regenerated on a later start.

Publisher endpoints accept only the dedicated source Bearer credential and no
browser Origin. Browser cookies, administrator credentials, and device tokens
are rejected; the publisher credential cannot access administrator or Agent
endpoints.

- GET /api/source/jobs -> {jobs:SourcePublication[]} with pending/failed frozen jobs.
- POST /api/source/jobs/{id}/complete {proof:SourceProof,parentCommit} -> {release:Release} (200). Parent, current ownership, frozen hash, and published baseline must match. A matching completed retry returns the same release; inconsistent proof returns 409.
- POST /api/source/jobs/{id}/fail {reason} -> {publication:SourcePublication} (200). Only `source_publish_failed` and `source_content_rejected` are accepted; arbitrary error text is rejected.

A publication request snapshots the saved draft. Later saves/restores remain
allowed; completion publishes the frozen content without replacing newer draft
text. One pending or failed request blocks another Publish. A retryable failure
retains the slot and previous published proof/version. Content rejection ends
the request and releases the slot for a corrected explicit publication. Devices
continue to receive only immutable Hub releases under their existing policy.

The Hub never checks out Git or executes a publisher command. A separately
reviewed external publisher establishes committed source/manifest validity and
then attests it through this API. The proof is not an independent Git audit,
cryptographic signature, device receipt, or application-loading result. Private
deployment-specific publisher tooling and credentials do not belong in the
public repository.

## Public Go APIs

internal/core defines exported JSON structs above, Version="0.1.0-alpha.1", Hash([]byte) lowercase SHA256, Token() cryptographic 32-byte hex and ValidID(string).

internal/hub owns store/server/static embed (UI agent only writes internal/hub/web/*).
- hub.Open(dataDir string) (*Store,error), Store.Close() error; persists db under 0700 data dir, 0600 DB. First init creates `admin-token` 0600 if no initialized credentials, never prints secret; subsequent missing token must not silently reset admin identity.
- hub.NewServer(store *Store, publicURL string) (http.Handler,error). publicURL optional for loopback development; origin validated against exact request origin or configured public URL. Embedded web assets served via go:embed web/*.
- hub.NewServerWithSources(store *Store, publicURL string, sources map[string]SourceConfig) (http.Handler,error) adds explicit local source ownership. SourceConfig has label and initialProof; Store.SourceTokenPath() returns the separate credential file location, not its contents.
- Store.AdminTokenPath() string optional helper.

internal/agent (root owns) uses HTTP contract, stores local config/state privately. CLI supports hub init, hub serve, agent enroll, agent bind, agent preview, agent once, agent run, agent status. User supplies tokens via files/stdin, not CLI arguments. Bind resolves app target locally and only explicit path or app default; no system remote discovery. Binding a populated target reports the adapter and fails on a foreign owned block. `agent preview --data DIR [--target ID] [--json]` uses the exclusive Agent lock, polls releases, and computes local diffs without writing files/state/recovery journals or sending receipts. Polling updates the Hub heartbeat. Conflicts and unsafe targets return no proposed write and cause nonzero exit. Preview is a snapshot; application fetches and validates again. Agent scheduler polls heartbeat every ~15s, per-pack auto intervals or manual trigger; first manual binding stays pending until trigger or explicit once. --once explicitly applies current published versions unless paused. Missing pack release pending. Paused must not apply even --once. Retry failures without bypassing conflict. Agent restart must retain interval/trigger state.

Managed block delimiter contract (own namespace only):
`<!-- rulesend:begin pack=ID version=N hash=HEX -->\nCONTENT\n<!-- rulesend:end -->`
Target has exactly zero or one owned block. Corrupt/duplicate/nested marker, wrong pack, local content hash drift -> conflict. Store exact generated block hash locally; re-read immediately before replacement, back up old whole file locally 0600, temp same directory + fsync + rename; verify new whole file and managed hash. Existing prefix/suffix bytes untouched. Target symlink or traversal forbidden (os.Root plus no-follow checks); parent must already exist; root/mapping safety state outside target folders. File permissions preserve existing; new 0600. Codex resulting file max 32KiB, Claude/custom max128KiB. Crash journal preserves pre/post hash and backup path; restart reconciles known results, ambiguous states conflict. Document filesystem atomicity limitations.

## Implementation boundaries and tests

`internal/core` defines the shared protocol. `internal/hub` owns persistence and HTTP authorization; `internal/hub/web` owns the English browser UI. `internal/agent` owns local file approval, application, and recovery. `cmd/rulesend` provides the CLI. See [the prototype plan](PRODUCT_PLAN.md) for scope and [validation](VALIDATION.md) for verified behavior.

Must verify draft CAS, single-use enrollment, revocation, scoped reads, receipt validation, stale/offline status, publish immutability, conflict preservation, untouched outside content, unscanned local edits, restart recovery and symlink rejection. Real secrets and personal/company paths never used as fixtures. Multi-device loopback simulation is not real separate-machine deployment; report distinction. Cross compilation is not OS runtime certification.
