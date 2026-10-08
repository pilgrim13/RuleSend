# RuleSend

**Your AI rules. Every device.**

Write shared AI instructions once, publish a version, and see which devices have actually installed it. RuleSend is a self-hosted Hub and Agent for **Codex, Claude Code, and other tools that read local Markdown instructions**.

[한국어](README.ko.md) · [Product plan](docs/PRODUCT_PLAN.ko.md) · [Commands](docs/CLI.md) · [Security model](docs/THREAT_MODEL.md)

> **Early alpha.** One administrator, macOS/Linux Agents, small Markdown rule packs. A file installation receipt is not proof that an AI app loaded the rules or followed them. Those indicators remain **unverified**.

## Why use this?

Git tells you what was committed. A copied file tells you what reached one folder. Neither, on its own, tells you whether every device received the version you intended.

RuleSend adds a deliberate publishing step and a view of the actual receiving devices:

- **One shared source:** edit Markdown in the Hub from any browser that can reach it.
- **Drafts stay drafts:** only an explicitly published, immutable version reaches Agents.
- **See the gap:** compare the published version with each target's last verified installation, connection time, and errors.
- **Keep existing rules:** only a clearly marked RuleSend section changes. Other content stays intact.
- **Keep local edits:** a modified managed section becomes a conflict instead of being silently overwritten.
- **Control delivery:** automatic intervals, manual sync requests, pause, scoped enrollment, and device revocation.
- **Small deployment:** one Go executable with an embedded web UI and SQLite. No Node server, cloud account, or Agent inbound port.

Use Git alongside RuleSend if you want code review or source history. RuleSend handles publishing and delivery observations. It is an original implementation, not a Syncthing fork or a general folder-sync engine.

```text
Browser ──> Hub: edit draft → publish v4
               │
               ├──> Laptop Agent → AGENTS.md   → file v4 verified
               └──> Desktop Agent → CLAUDE.md  → local conflict

                    App loading / AI behavior → unverified
```

## Try it locally

Requires Go **1.25 or newer**, Git, and macOS or Linux. Source installation is the initial supported route. A release workflow is included; do not assume a binary release exists until it appears on this repository's Releases page.

### 1. Build and start the Hub

```sh
git clone https://github.com/pilgrim13/rulesend.git
cd rulesend
go test ./...
go build -trimpath -o rulesend ./cmd/rulesend
./rulesend hub init --data "$HOME/.local/share/rulesend-hub"
./rulesend hub serve --data "$HOME/.local/share/rulesend-hub"
```

Open **http://127.0.0.1:8787**. The initialization command prints the location of a private `admin-token` file, not its contents. Open that file locally and enter the token in the login screen. Do not paste it into an AI chat, issue, screenshot, or shell command argument.

Create a pack such as **Shared working rules**, write a short Markdown draft, save it, and **Publish**. Keep passwords, company material, and machine-specific paths out of shared rules.

### 2. Enroll a device

In **Devices → Add device**, select the published pack and create a short-lived, single-use enrollment token. On the receiving device, save it with a local text editor in a private file, then restrict that file's permissions. Replace the example path below with your actual file.

```sh
chmod 600 /absolute/private/enrollment-token
./rulesend agent enroll \
  --data "$HOME/.local/share/rulesend-agent" \
  --hub http://127.0.0.1:8787 \
  --name "My laptop" \
  --token-file /absolute/private/enrollment-token
```

The token file may be removed after successful enrollment. `--token-file -` also accepts standard input. Each device needs its own enrollment token. For a different physical device, use your Hub's **HTTPS** address; `127.0.0.1` always means the current device.

### 3. Approve the local files

Copy the pack ID from the Hub. Start with a test project. The parent folder must already exist.

```sh
./rulesend agent bind \
  --data "$HOME/.local/share/rulesend-agent" \
  --pack PACK_ID --adapter codex \
  --path /absolute/path/to/test-project/AGENTS.md \
  --label "Test project Codex"

./rulesend agent bind \
  --data "$HOME/.local/share/rulesend-agent" \
  --pack PACK_ID --adapter claude \
  --path /absolute/path/to/test-project/CLAUDE.md \
  --label "Test project Claude"

./rulesend agent once --data "$HOME/.local/share/rulesend-agent"
./rulesend agent status --data "$HOME/.local/share/rulesend-agent"
```

Binding saves a local mapping without changing the target. `once` applies current published versions and verifies the result. Existing content is retained outside the managed block. One target file holds one RuleSend pack. A pack may have many targets.

For explicitly chosen **global** rules, use `--default` instead of `--path`: Codex resolves to `~/.codex/AGENTS.md`, Claude Code to `~/.claude/CLAUDE.md`. RuleSend does not discover every workspace, import another tool's blocks, or override company policy. App configuration can change which files are read; verify that separately in the app.

### 4. Keep the Agent running

```sh
./rulesend agent run --data "$HOME/.local/share/rulesend-agent"
```

The Agent checks in about every 15 seconds. Each pack controls its application interval (15 seconds to 24 hours), manual mode, and pause state. **Sync now** requests an apply on the next contact; it does not bypass pause. `once` explicitly applies manual packs but also respects pause.

Stop `run` before another mutating Agent command. `status` can read saved observations while `run` is active. To start automatically after login, use the reviewed templates in [operations](docs/OPERATIONS.md).

## Read the status correctly

| Signal | What it actually means |
| --- | --- |
| Published v4 | The Hub accepted that immutable version. |
| File applied / verified | The Agent read the local file back and reported the expected version and hash. |
| Last seen | The last authenticated Agent contact; separate from file status. |
| Stale / offline | The old receipt is historical. Current disk contents are unknown. |
| Conflict | A local managed section changed, disappeared, or became ambiguous. It was preserved. |
| App loaded / behavior | Always unverified in this alpha. A fresh app session still needs its own check. |

Claude **Code** is supported through its local instruction file. Claude general chat, Projects, and Cowork are not automatically configured. RuleSend does not refresh existing conversations. Codex adapters do not certify every rule-discovery setting or eliminate instructions inherited from other locations.

## Editing, conflicts, and rollback

Edit and save a draft in the Hub, then publish when ready. Concurrent edits use revision checks; a stale save is rejected instead of overwriting a newer draft. A previous release can be **restored to the draft**, reviewed, and published as a new version.

Local managed-block edits are never uploaded automatically. On a conflict, save your local edits separately, compare them with the release reported by `agent status`, and restore only that managed section to its known version before retrying. If you want the local change everywhere, copy the intended text into a Hub draft and publish it. Malformed or missing markers require local review; the alpha has no automatic “force overwrite” button.

Before a replacement the Agent keeps an owner-only whole-file backup and a recovery journal in its data directory. Backups are not automatically pruned. Do not copy an old whole-file backup over newer unrelated edits without comparing them. See [recovery instructions](docs/OPERATIONS.md#recovery).

## Remote hosting and security

The default listener is loopback only. Remote access requires HTTPS, using the built-in TLS flags or a correctly configured HTTPS reverse proxy. Example configuration, startup templates, and backup guidance are in [operations](docs/OPERATIONS.md).

The Hub can read your rules. This is not end-to-end encryption against the Hub owner. A device receives only its enrolled packs, and the Hub cannot add a local path or execute a shell command. Keep the Hub and Agent data directories private and out of Git.

Alpha limits: one administrator; 100 targets per Agent; pack content up to 128 KiB; final Codex file up to 32 KiB; other final files up to 128 KiB. File modes are retained; ACLs, extended attributes, special files, and symlinks are not supported. Independent editors can race any filesystem write, so universal loss-free concurrent editing is not promised.

## Install with Claude or Codex

Copy this prompt into your coding assistant. It deliberately leaves credential entry to you and starts with a test target.

```text
Help me install RuleSend from https://github.com/pilgrim13/rulesend.

1. Confirm this machine, OS, current folder, repository owner, and the version
   or commit we will build. Read README.md, docs/CLI.md, and docs/OPERATIONS.md.
2. Explain whether this machine will run the Hub, an Agent, or both. If the Hub
   URL or target scope is unknown, ask only for that missing information.
3. Review the source/install commands, check the supported Go version, then
   run tests and build. Do not run an unreviewed curl-to-shell installer.
4. Keep all runtime data outside the checkout with owner-only permissions.
   Do not read, print, upload, or commit admin/device/enrollment tokens. Have
   me enter credentials locally through the UI or a private token file.
5. Begin with a small synthetic rule pack and an explicitly approved test
   AGENTS.md or CLAUDE.md. Preserve existing text and other managed blocks.
   Do not copy entire global settings folders or private/company documents.
6. Apply one published version, check the actual file and Hub receipt, then
   show a safe local-edit conflict. Explain the rollback and backup steps.
7. Before configuring automatic startup or global targets, show the exact
   paths and changes. Use HTTPS for a remote Hub; do not bypass TLS checks.
8. Report Hub setup, each target's file installation, app loading, and AI
   behavior separately. Leave untested stages marked unverified.
```

A [Korean installation prompt](README.ko.md#ai에게-설치-맡기기) is included in the Korean README.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/rulesend
```

Tests cover authenticated publishing, concurrent revisions/enrollment, scope and revocation, two local Agents, managed-block preservation, local conflicts, policy handling, filesystem safety, and interrupted-write recovery. Two Agents in a local integration test are not two physically deployed computers. See [validation](docs/VALIDATION.md) for the actual checked environments.

Contributions: [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md). Roadmap and acceptance criteria: [product plan](docs/PRODUCT_PLAN.ko.md).

## License

RuleSend's original code is [MIT licensed](LICENSE). Dependency licenses are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). No Syncthing source is included; Syncthing's MPL license does not govern this independent implementation.
