# Command contract

RuleSend is one executable with Hub and Agent roles. macOS and Linux are the alpha deployment targets; Windows self-hosting and Agent runtime are not supported in this release. Commands never accept credentials directly as command-line flags. Run `rulesend --help` for the command summary.

```sh
rulesend version
rulesend hub init --data ./rulesend-hub
rulesend hub serve --data ./rulesend-hub --listen 127.0.0.1:8787
rulesend agent enroll --data ./rulesend-agent --hub http://127.0.0.1:8787 --name "My laptop" --token-file /private/path/enrollment-token
rulesend agent bind --data ./rulesend-agent --pack PACK_ID --adapter codex --path /absolute/path/AGENTS.md --label "Project · Codex"
rulesend agent preview --data ./rulesend-agent
rulesend agent once --data ./rulesend-agent
rulesend agent run --data ./rulesend-agent
rulesend agent status --data ./rulesend-agent
```

The Hub stores its initial administrator token in `DATA/admin-token` with owner-only permissions. Enter its contents into the browser login; do not paste it into issues or chat. Agent enrollment accepts `--token-file -` for standard input. The Hub creates a scoped, short-lived, single-use enrollment token in its web UI. The Agent writes its replacement device credential to its private data directory.

Bindings require an explicit existing parent directory. `--adapter` accepts `codex`, `claude`, or `markdown`. `--path` explicitly approves one file. As an alternative, `--default` selects `~/.codex/AGENTS.md` or `~/.claude/CLAUDE.md` for the corresponding adapter. No folder is scanned and no existing rules are uploaded. One file can contain one RuleSend pack; other tools' managed sections remain untouched.

Choose exactly one of `--path` and `--default`; `--default` is unavailable for `markdown`. The default paths are explicit file choices, not discovery of every application configuration. If an application uses a custom configuration location, use `--path`. An existing RuleSend block cannot be adopted without its original Agent state. Binding saves the target and prints its path, pack and adapter without modifying the application file. Run `agent preview` to review the actual local before/after content before applying.

Pack content is limited to 128 KiB of UTF-8 Markdown. The resulting whole target file, including unrelated existing content and RuleSend markers, must fit within 32 KiB for `codex` or 128 KiB for `claude`/`markdown`.

`agent once` explicitly requests all current published versions, including manual packs, but respects pause. `agent run` heartbeats approximately every 15 seconds and follows each pack's automatic interval (15–86,400 seconds) or manual trigger. Binding contacts the Hub and persists the current trigger as its baseline. For initial manual installation, run `agent once` or request Sync now after binding. Sync now does not unpause a pack. Configuration and scheduling state survive restart.

Only one mutating Agent command can use a data directory at a time. Preview also takes this lock for a coherent local baseline. Stop `run` before enrolling, binding, previewing, or running `once` with that directory. `agent status` is a read-only view of the last saved observations and can run while the daemon is active; it does not freshly read each application file.

For a remote Hub use HTTPS. The Hub supports `--tls-cert`, `--tls-key`, and `--public-url https://rules.example.com`. Direct non-loopback listening requires both TLS files and an explicit HTTPS public origin. When using a trusted HTTPS reverse proxy, keep the Hub bound to loopback and set its public URL. Do not bind an unencrypted listener to a non-loopback address. Agents verify TLS certificates and do not follow Hub redirects.

Draft editing and publishing are separate actions. Restoring a previous release creates a draft; publish it to produce a new immutable version. A local edit to a managed block is a conflict. The Agent does not silently import or discard that edit. Backups and a replacement journal live in its data directory. Existing conversations and application behavior are not automatically verified.

## Optional source ownership

```sh
rulesend hub serve --data /absolute/private/hub-data \
  --listen 127.0.0.1:8787 --public-url https://rules.example.com \
  --source-config /absolute/private/source-config.json
```

`--source-config` accepts one owner-only regular JSON file, at most 64 KiB. Its
top-level keys are existing pack IDs; each value has this shape. Replace the
placeholders with independently verified source metadata before starting:

```json
{
  "PACK_ID": {
    "label": "Reviewed shared rules",
    "initialProof": {
      "commit": "<40-character-lowercase-commit>",
      "releaseId": "rules-v1",
      "version": "v1",
      "hash": "<64-character-lowercase-sha256>"
    }
  }
}
```

The initial hash must match an existing published pack. This config contains no
Git command, repository path, or publisher token. Keep its original anchor on
subsequent starts even after completed publications advance the current proof.
An already enabled owner cannot be omitted or silently changed.

For a source-owned pack, explicit Publish returns a queued request. A separately
reviewed external publisher records its frozen saved content and submits the
source proof before a Hub release is created. Newer drafts remain independent.
The Hub creates a private `source-token` for this dedicated machine role; do not
reuse administrator or device credentials. RuleSend provides the job API, not a
general Git worker or remote shell. See [operations](OPERATIONS.md) for failure
preservation and [the API contract](IMPLEMENTATION_CONTRACT.md) for endpoints.

Ordinary packs are unchanged. No Agent flags or additional local path permissions
are granted by enabling source ownership.

This alpha has no unbind or remote deletion command. Stop the Agent to stop local processing, pause a pack to stop its application, or revoke a device to reject its future Hub requests. These actions leave installed application files in place. Preserve the Agent data directory and backups when resolving a conflict; deleting state can prevent recognition of an existing managed block. The service does not promise lossless behavior against every unrelated local writer.

## Preview before applying

```sh
rulesend agent preview --data ./rulesend-agent
rulesend agent preview --data ./rulesend-agent --target TARGET_ID --json
```

Preview fetches published releases and compares them with locally approved files. It does not change application files, configuration, scheduling state, backups, or recovery journals, and sends no installation receipts or local file contents. The Hub records the polling heartbeat. Stop `agent run` first; preview holds the same exclusive Agent lock. An existing recovery journal blocks preview until normal recovery is completed.

| Status | Meaning |
| --- | --- |
| `ready` | The displayed change can be requested with `agent once`. |
| `current` | The file already matches the published release. |
| `paused` | A prospective change is shown, but application remains paused. |
| `pending` | No published release is available. |
| `conflict` | The managed block differs from its trusted local baseline. No proposed write is shown. |
| `blocked` | The target, recovery state, scope, or release could not be safely verified. No proposed write is shown. |

Any conflict or blocked target makes the command exit unsuccessfully after printing results. `--target` filters by the local target ID shown by `agent status`. `--json` prints the same structured results without credentials; paths and diff content can still be private, so do not upload the output without review. Recorded version means the saved Agent baseline, not a new installation claim. Before/after hashes cover whole files; the hash for a missing file is the empty-content hash, with `createsFile` identifying creation.

The display diff escapes terminal control characters and uses a single hunk; it is not a patch file. Preview is a snapshot, not a reservation: `once` fetches the latest release and rechecks the file. App loading and AI behavior remain unverified.

## Run Prototype 1 locally

From a source checkout with Go 1.25+ installed:

```sh
./scripts/run-prototype.sh
```

This builds the current source and starts a real Hub at `http://127.0.0.1:8787`. Its private data lives outside the checkout at `${XDG_DATA_HOME:-$HOME/.local/share}/rulesend-prototype`. Initialization prints the location of the administrator token file, never the token. Enter its contents locally in the login screen, create a rule pack, publish it, and use the in-app Agent setup guide for explicit test targets. No sample devices or application bindings are created automatically.

Stop with Ctrl+C; restarting reuses the same private Hub data. Set `RULESEND_PROTOTYPE_DATA`, `RULESEND_PROTOTYPE_LISTEN`, or `RULESEND_GO_BIN` to override the private data folder, listen address, or Go executable. The normal Hub TLS/listening restrictions still apply. This helper is for local prototype development, not a service installation.
