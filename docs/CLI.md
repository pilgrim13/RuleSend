# Command contract

RuleSend is one executable with Hub and Agent roles. macOS and Linux are the alpha deployment targets; Windows self-hosting and Agent runtime are not supported in this release. Commands never accept credentials directly as command-line flags. Run `rulesend --help` for the command summary.

```sh
rulesend version
rulesend hub init --data ./rulesend-hub
rulesend hub serve --data ./rulesend-hub --listen 127.0.0.1:8787
rulesend agent enroll --data ./rulesend-agent --hub http://127.0.0.1:8787 --name "My laptop" --token-file /private/path/enrollment-token
rulesend agent bind --data ./rulesend-agent --pack PACK_ID --adapter codex --path /absolute/path/AGENTS.md --label "Project · Codex"
rulesend agent once --data ./rulesend-agent
rulesend agent run --data ./rulesend-agent
rulesend agent status --data ./rulesend-agent
```

The Hub stores its initial administrator token in `DATA/admin-token` with owner-only permissions. Enter its contents into the browser login; do not paste it into issues or chat. Agent enrollment accepts `--token-file -` for standard input. The Hub creates a scoped, short-lived, single-use enrollment token in its web UI. The Agent writes its replacement device credential to its private data directory.

Bindings require an explicit existing parent directory. `--adapter` accepts `codex`, `claude`, or `markdown`. `--path` explicitly approves one file. As an alternative, `--default` selects `~/.codex/AGENTS.md` or `~/.claude/CLAUDE.md` for the corresponding adapter. No folder is scanned and no existing rules are uploaded. One file can contain one RuleSend pack; other tools' managed sections remain untouched.

Choose exactly one of `--path` and `--default`; `--default` is unavailable for `markdown`. The default paths are explicit file choices, not discovery of every application configuration. If an application uses a custom configuration location, use `--path`. An existing RuleSend block cannot be adopted without its original Agent state. Binding saves the target and prints its path, pack and adapter; it does not modify the application file or display a full content diff. Review the published pack and existing file before applying. Full before/after preview is a future improvement.

Pack content is limited to 128 KiB of UTF-8 Markdown. The resulting whole target file, including unrelated existing content and RuleSend markers, must fit within 32 KiB for `codex` or 128 KiB for `claude`/`markdown`.

`agent once` explicitly requests all current published versions, including manual packs, but respects pause. `agent run` heartbeats approximately every 15 seconds and follows each pack's automatic interval (15–86,400 seconds) or manual trigger. Binding contacts the Hub and persists the current trigger as its baseline. For initial manual installation, run `agent once` or request Sync now after binding. Sync now does not unpause a pack. Configuration and scheduling state survive restart.

Only one mutating Agent command can use a data directory at a time. Stop `run` before enrolling, binding, or running `once` with that directory. `agent status` is a read-only view of the last saved observations and can run while the daemon is active; it does not freshly read each application file.

For a remote Hub use HTTPS. The Hub supports `--tls-cert`, `--tls-key`, and `--public-url https://rules.example.com`. Direct non-loopback listening requires both TLS files and an explicit HTTPS public origin. When using a trusted HTTPS reverse proxy, keep the Hub bound to loopback and set its public URL. Do not bind an unencrypted listener to a non-loopback address. Agents verify TLS certificates and do not follow Hub redirects.

Draft editing and publishing are separate actions. Restoring a previous release creates a draft; publish it to produce a new immutable version. A local edit to a managed block is a conflict. The Agent does not silently import or discard that edit. Backups and a replacement journal live in its data directory. Existing conversations and application behavior are not automatically verified.

This alpha has no unbind or remote deletion command. Stop the Agent to stop local processing, pause a pack to stop its application, or revoke a device to reject its future Hub requests. These actions leave installed application files in place. Preserve the Agent data directory and backups when resolving a conflict; deleting state can prevent recognition of an existing managed block. The service does not promise lossless behavior against every unrelated local writer.
