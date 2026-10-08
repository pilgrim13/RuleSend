# Operating RuleSend

Run the Hub and each Agent as a normal, dedicated user where practical. Keep executables and runtime state separate. Do not put the Hub database, admin token, Agent configuration, recovery journals, or backups inside a synced folder or source checkout.

## HTTPS and network access

Default local development:

```sh
rulesend hub serve --data /absolute/private/hub-data --listen 127.0.0.1:8787
```

Direct TLS with a valid certificate for your host:

```sh
rulesend hub serve --data /absolute/private/hub-data \
  --listen 0.0.0.0:8787 --public-url https://rules.example.com:8787 \
  --tls-cert /absolute/private/fullchain.pem \
  --tls-key /absolute/private/privkey.pem
```

Or bind the Hub to loopback behind a same-machine HTTPS reverse proxy:

```sh
rulesend hub serve --data /absolute/private/hub-data \
  --listen 127.0.0.1:8787 --public-url https://rules.example.com
```

Example Caddy configuration (replace the domain and configure its DNS/firewall yourself):

```caddyfile
rules.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

The public URL must exactly match the browser origin, including a non-default port. The UI uses same-origin requests and does not support mounting under a URL subpath. Do not expose the plaintext backend to a network, disable certificate verification, put tokens in URLs, or log request bodies/authorization headers at a proxy. Agents reject redirects and verify HTTPS certificates. A private network/VPN is a sensible additional boundary; its setup is outside this alpha.

## Start automatically

First complete a successful manual enrollment, binding, `agent preview`, and `agent once`. Then install the reviewed executable at a stable path you control. The templates below do not enroll a device or grant a new path. Replace every placeholder and inspect the resulting file before enabling it.

### Linux user service

Create `~/.config/systemd/user/rulesend-agent.service`:

```ini
[Unit]
Description=RuleSend rule delivery agent
After=network-online.target

[Service]
ExecStart=/ABSOLUTE/PATH/rulesend agent run --data /ABSOLUTE/PRIVATE/agent-data
Restart=on-failure
RestartSec=15
UMask=0077
NoNewPrivileges=true

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now rulesend-agent.service
systemctl --user status rulesend-agent.service
```

This normally follows the user's login lifecycle. System-wide operation or lingering requires a separate local policy decision. Stop it with `systemctl --user stop rulesend-agent.service` before changing bindings or running a local preview. Use `disable --now` to remove automatic startup.

### macOS LaunchAgent

Create `~/Library/LaunchAgents/io.rulesend.agent.plist`, using literal absolute paths (launchd does not expand `~` or `$HOME` inside this XML):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.rulesend.agent</string>
  <key>ProgramArguments</key><array>
    <string>/ABSOLUTE/PATH/rulesend</string>
    <string>agent</string><string>run</string>
    <string>--data</string><string>/ABSOLUTE/PRIVATE/agent-data</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>15</integer>
  <key>Umask</key><integer>63</integer>
</dict></plist>
```

```sh
plutil -lint "$HOME/Library/LaunchAgents/io.rulesend.agent.plist"
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/io.rulesend.agent.plist"
launchctl print "gui/$(id -u)/io.rulesend.agent"
```

Stop/unload with `launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/io.rulesend.agent.plist"`. Remove the plist yourself if you no longer want it loaded on login. These service templates are examples, not an automatically installed service or a claim that service-manager behavior was tested on every system.

## Backup

Stop the Hub cleanly and copy its entire private data directory to protected backup storage. The alpha uses SQLite's rollback journal; copying a live database without a consistent snapshot is not a supported backup procedure. Include the original `admin-token`: restarting an initialized Hub with that token missing or mismatched deliberately fails instead of replacing its identity.

Stop an Agent before backing up its entire data directory. It contains a device credential, mappings, scheduling state, recovery journals, and potentially sensitive whole-file backups. Protect and retain it accordingly. Backups are not automatically deleted; monitor disk usage. Copies of rule files alone do not preserve the Agent's trusted baseline.

## Recovery

1. Pause the affected pack or stop the Agent. Preserve the current file, Agent state, and backup directory.
2. Run `agent status` to read the last observation and identify the target, version, and failure. This is a saved observation, not a fresh application-file inspection.
3. With the Agent stopped, `agent preview` can inspect the current release without changing files or sending receipts. It refuses pending recovery journals; preserve those for normal recovery. Compare the current file with the relevant backup and the immutable Hub release. Local backups are in `AGENT_DATA/backups/`. Do not blindly restore a whole file over unrelated edits made since the backup.
4. For a managed-content conflict, preserve your local change separately. Restore the managed section to the version recorded in Agent state, including its original content and markers. Keep outside text intact. If the desired change should be shared, put it in a new Hub draft and publish it.
5. Retry `agent once`. Confirm both the local file and the new Hub receipt. App loading and behavior still need separate checks.

An interrupted replacement leaves `journal-TARGET_ID.json` with pre/post hashes and a backup reference. On restart, the Agent recognizes the exact pre-image or post-image. An ambiguous file is preserved and requires local review. Do not delete a journal or reset state merely to make an error disappear. The alpha does not offer an automatic repair/force command.

After restoring an old Hub backup, keep delivery paused until device histories are reconciled. An Agent rejects a published version lower than its installed version, or a different hash at the same version. Restore the newer consistent Hub backup when available. Recreate intentional content as newer published versions only after review. Old receipts are historical and must not be treated as proof of current disk contents.

## Credentials, scope, and removing an installation

- The admin token is a powerful shared administrator secret. Browser sessions expire; log out on shared computers. Built-in admin rotation and multi-admin accounts are not available in this alpha.
- An enrollment token expires within the selected period and works once. Device credentials are stored privately on the Agent. Revoke a device in the UI to block subsequent requests; already downloaded rules remain on disk.
- Enrollment pack scope is fixed for that device. To change scope in this alpha, revoke and re-enroll with a new private data directory. Existing managed sections cannot be adopted automatically; preserve and review the original Agent state and files before migrating.
- Stop the Agent and disable its service before uninstalling. The alpha has no unbind, remote file deletion, or automatic cleanup command. Removing the executable does not erase existing rules or protected backups.
- `--default` chooses the conventional app path. If `CODEX_HOME` or `CLAUDE_CONFIG_DIR` is set, RuleSend requires an explicit `--path` so the real destination is reviewed.
- Limits are enforced per pack/file and per request. Audit history and backups consume disk; there is no automatic retention policy or storage quota. Monitor free space and keep independent backups.

## Release process

Run the test and review checklist, then tag a reviewed commit as `vX.Y.Z`. The repository workflow builds macOS/Linux archives and SHA-256 checksums. Each archive includes the original license and dependency notices. Review the workflow and generated assets before announcing a release. Checksums detect accidental corruption; they do not independently establish trust in a compromised publisher. Code signing, notarization, and auto-update are later work.
