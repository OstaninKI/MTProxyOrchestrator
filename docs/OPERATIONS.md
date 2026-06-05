# Operations

## Prerequisites

- Ubuntu 22.04 or later
- Root privileges
- Free ports `80` and `443`
- Outbound HTTPS access to GitHub Releases

## Current Install Flow

Interactive mode:

```bash
sudo tgproxy-cli install
```

Current interactive flow asks for:

1. FakeTLS mask host
2. optional panel domain and Let's Encrypt email
3. final confirmation for a Single-mode install

Unattended mode:

```bash
sudo tgproxy-cli install --unattended
```

The current install path generates:

- panel path
- admin login
- admin password
- first MTProto secret

It then installs:

- `tgproxy-cli`
- `tgproxy-panel`
- `teleproxy`
- `teleproxy` system user
- nginx stub site configuration
- nginx loopback TLS stub site configuration when public panel TLS is configured
- `teleproxy.service`
- `tgproxy-panel.service`
- `/etc/tgproxy/teleproxy.toml`
- `/etc/tgproxy/secrets/users.json`
- `/etc/tgproxy/panel.db`

Generated panel credentials and the first user link are printed after the install health check result. If the final health check fails, the generated access details are still printed for recovery, and the CLI points the operator at the Teleproxy journal for diagnosis.

## Current Runtime Model

### Single mode

Current install flow provisions Single mode only.

```text
Telegram client -> Teleproxy :443 -> Telegram DC
Unknown probes/browser traffic -> nginx stub on loopback
```

When the public panel TLS flags are used, Teleproxy's FakeTLS fallback points at a loopback HTTPS stub on `127.0.0.1:9443` using the panel certificate. This keeps the public mask host and the fallback certificate aligned for domain installs.

### Bridge mode

Bridge support exists in the runtime and admin panel code, but it is not part of the current initial CLI install flow. The intended operational path today is:

1. install in Single mode
2. log into the panel backend
3. add/enable Bridge nodes from the panel

When active, Bridge uses:

- `/etc/tgproxy/sing-box.json`
- `/etc/tgproxy/nodes/outbounds.json`
- `sing-box.service`

## Panel Backend Access

The current panel process listens on loopback only:

- address: `127.0.0.1:18080`
- protocol: plain HTTP
- health endpoint: `http://127.0.0.1:18080/<generated-panel-path>/health`

The authenticated UI is mounted under the generated random path printed during install.
Dashboard navigation and subpage "Dashboard" links are generated from that path, so deployments mounted under `/p-.../` do not leak users back to the site root.

### Remote access

The install path can publish the panel on a public TLS endpoint in two ways:

1. `--panel-domain`, `--panel-cert`, and `--panel-key` use operator-managed certificate files.
2. `--panel-domain` and `--panel-email` obtain a Let's Encrypt certificate during install and enable renewal from the panel service.

The panel backend remains plain HTTP on loopback (`127.0.0.1:18080`); nginx terminates public TLS on port `8443` when public panel flags are used.

## Dashboard Observability

The authenticated Dashboard is an operator console for service state, traffic, and Teleproxy runtime signals. It refreshes private fragments through authenticated SSE events and does not expose metric data on unauthenticated routes.

### Data sources

- Host state is read best-effort from local Linux files such as `/proc/meminfo`, `/proc/loadavg`, `/proc/uptime`, and `statfs("/")`.
- Service health uses mode-aware checks: Single mode checks Teleproxy, panel, and nginx stub health; Bridge mode checks Teleproxy, sing-box, SOCKS5 inbound, and Telegram chain health.
- Traffic history is stored in SQLite from Teleproxy cumulative counters and rendered as sampled deltas.
- Live runtime data is scraped from Teleproxy `/metrics` on the configured loopback stats port. The scraper uses an explicit HTTP timeout and a 1 MiB response cap.

### Dashboard sections

- **Network throughput** shows upload/download history for the selected `1h`, `24h`, `7d`, or `30d` window. The chart also overlays historical active connection counts from the same traffic buckets.
- **System** shows memory, disk, load average, uptime, kernel, and active runtime mode.
- **Services & Components** shows installed component versions and mode-aware component status.
- **Connection quality** shows Teleproxy accepted and rejected connection counters, SOCKS5 upstream attempt/success/failure counters, and the top JA4 ClientHello fingerprints when available.
- **Top users by traffic** shows sampled upload, download, total traffic, and peak connections for the selected period.
- **Bridge nodes** shows the configured Bridge node preview, enabled state, and last latency test result.
- **Active Connections** shows current per-secret connection count, live upload/download counters, Teleproxy connection/IP limits, and per-secret rejection reasons.

### Teleproxy metrics currently consumed

The panel parses the following current Teleproxy metric names:

- per-secret traffic and connections: `teleproxy_secret_bytes_received_total`, `teleproxy_secret_bytes_sent_total`, `teleproxy_secret_connections`
- per-secret limits and IP usage: `teleproxy_secret_connection_limit`, `teleproxy_secret_unique_ips`, `teleproxy_secret_max_ips`
- per-secret rejection reasons: `teleproxy_secret_connections_rejected_total`, `teleproxy_secret_rejected_quota_total`, `teleproxy_secret_rejected_ips_total`, `teleproxy_secret_rejected_expired_total`
- accepted and rejected connection counters: `teleproxy_ext_connections_created_total`, `teleproxy_connections_failed_lru_total`, `teleproxy_connections_failed_flood_total`, `teleproxy_ip_acl_rejected_total`
- Bridge/SOCKS5 upstream counters: `teleproxy_socks5_connects_attempted_total`, `teleproxy_socks5_connects_succeeded_total`, `teleproxy_socks5_connects_failed_total`
- probe visibility: `teleproxy_ja4_seen{hash=...}`

Legacy `mtproto_secret_traffic_bytes_in`, `mtproto_secret_traffic_bytes_out`, and `mtproto_secret_connections_total` names are still accepted for traffic sampling compatibility.

### Operational notes

- Teleproxy metrics are cumulative counters. The dashboard stores traffic deltas for history and quotas, while the live Connection quality card displays the latest raw counters from the current Teleproxy process.
- JA4 hashes are useful for diagnosing active probing and DPI signature changes. Treat them as operational telemetry; they are not MTProto secrets, but they should stay behind the authenticated panel.
- The SOCKS5 upstream counters are most useful in Bridge mode. In Single mode they may remain empty or show `n/a`.

## User Traffic Quotas

The panel can enforce per-user traffic quotas. Quotas are optional; a `quota_bytes` value of zero means unlimited.

Setting a quota:

1. open the Users section in the panel
2. on the target user, set quota size in bytes, the period (`daily`, `weekly`, or `monthly`), and the warning percentage (default `80`)
3. save

Behavior:

- A background service recomputes usage every 5 minutes from `traffic_daily` since the current period start.
- When usage crosses the warning percentage, a single `quota_warning` event is written to the audit log for the period.
- When usage exceeds the quota, the user is suspended automatically: their secret is excluded from the rendered Teleproxy config and the service is reloaded.
- Period rollover resets `quota_used_bytes`, clears suspension, and advances `quota_period_start`.

Manual actions on the user record:

- **Reset quota counters**: clears `quota_used_bytes` and `quota_warned`, lifts suspension if it was triggered by the quota.
- **Toggle suspend**: forces the user into or out of the suspended state. Suspending triggers a Teleproxy reload immediately.

Audit events: `quota_set`, `quota_reset`, `quota_warning`, `user_suspend_toggle`.

## Two-Factor Authentication For Admins

TOTP-based 2FA is opt-in per admin account.

Enable 2FA:

1. log into the panel
2. open the panel Settings page and find the "Two-factor authentication" section
3. start enrollment; the panel shows a QR code and the otpauth URL
4. scan the QR with an authenticator app (Aegis, 1Password, Google Authenticator, etc.)
5. confirm with one valid 6-digit code from the app
6. record the eight recovery codes shown once on the success screen; store them outside the panel host

After enrollment, every login goes through password verification followed by a TOTP step at `/totp/verify`. A 5-minute pending-TOTP cookie is issued between the two steps. Failed TOTP attempts share the same rate limiter as failed passwords (5 per IP per 5 minutes, then 1 hour block).

Disable 2FA:

1. open Settings, "Two-factor authentication"
2. submit the disable form with a current TOTP code or an unused recovery code

Regenerate recovery codes from the same section; the action also requires a current TOTP or recovery code and invalidates previous codes.

### Lost 2FA Device Recovery

If the authenticator app is lost:

1. use one of the recorded recovery codes during the `/totp/verify` step or in the disable form
2. once logged in, regenerate recovery codes and re-enroll with a new authenticator

If all recovery codes are also lost, reset 2FA from the host using the CLI:

```bash
sudo tgproxy-cli reset-totp
sudo systemctl restart tgproxy-panel
```

The command clears `totp_enabled`, `totp_secret`, and `totp_recovery_codes` for the admin row. Pass `--yes` to skip the confirmation prompt in unattended environments.

If the `tgproxy-cli` binary is unavailable, fall back to direct SQLite access:

```bash
sudo systemctl stop tgproxy-panel
sudo sqlite3 /etc/tgproxy/panel.db \
  "UPDATE admin SET totp_enabled = 0, totp_secret = '', totp_recovery_codes = '';"
sudo systemctl start tgproxy-panel
```

After recovery, log in with the password, re-enroll TOTP, and rotate the admin password using `tgproxy-cli reset-admin-password`.

Audit events: `totp_enabled`, `totp_disabled`, `totp_recovery_regenerated`, `totp_recovery_used`, `totp_failed`.

## Reset Admin Credentials

```bash
sudo tgproxy-cli reset-admin-password
```

The command generates and prints a new random login and password, updates the bcrypt hash in `/etc/tgproxy/panel.db`, and requires a panel restart to take effect.

## Status

```bash
sudo tgproxy-cli status
```

Reports current service health for the active runtime mode. Single mode checks Teleproxy, the panel service, and nginx stub health. Bridge mode checks the sing-box chain.

## Restart

```bash
sudo tgproxy-cli restart
```

Restarts all managed services for the active mode and verifies each one reaches `active` state within 15 seconds. Reports a table of service names and status (OK/FAILED). Returns a non-zero exit code if any service fails the health check.

In Single mode: `teleproxy.service`, `tgproxy-panel.service`.
In Bridge mode: `teleproxy.service`, `tgproxy-panel.service`, `sing-box.service`.

The command detects the current mode from the Teleproxy config at `/etc/tgproxy/teleproxy.toml`.

## Backup

Create an encrypted backup:

```bash
sudo tgproxy-cli backup --dest /root/tgproxy-backup.enc --passphrase 'strong-passphrase'
```

Backups include configuration/state required to reconstruct `/etc/tgproxy/`. They do not include packaged system dependencies.

## Restore

Restore from an encrypted archive:

```bash
sudo tgproxy-cli restore /root/tgproxy-backup.enc --passphrase 'strong-passphrase'
```

The restore flow:

1. asks for confirmation
2. stops managed services
3. validates archive structure and safety
4. restores into `/etc/tgproxy/`
5. restarts services

## DPI Resistance

Teleproxy supports several DPI-resistance features that are configurable in the admin panel under **Settings → Proxy Settings**. The settings are stored in the `settings` table of the panel DB and are mirrored as `config.toml` keys. Changes take effect after saving; the panel re-renders `teleproxy.toml` and restarts Teleproxy automatically.

### Settings reference

| Panel label | Key | Type | Default | Purpose |
|---|---|---|---|---|
| ClientHello MSS clamp | `mss_clamp` | bool | `true` | Forces TCP MSS clamping so that the TLS ClientHello is fragmented across TCP segments, defeating segment-level DPI inspection. |
| JA4 probe logging | `ja4_log` | bool | `true` | Logs a per-connection JA4 fingerprint on Teleproxy's `/stats` and `/metrics` endpoints. Useful for diagnosing which client TLS stacks are being fingerprinted. |
| Padded (dd) links | `random_padding` | bool | `false` | Selects the share-link transport (see below). Default `false` means Fake-TLS links are generated. |
| TLS backend | `tls_backend` | string | _(empty)_ | Address (e.g. `127.0.0.1:9443` or `proxy.example.com:443`) where Teleproxy forwards invalid probe connections. When empty, the built-in stub is used. |
| Wildcard mask | `wildcard_mask` | string | _(empty)_ | Wildcard masquerade hostname (e.g. `*.example.com`). Requires `tls_backend` to be set. |

### Share-link transport: Fake-TLS vs random padding

**Fake-TLS** (`random_padding = false`, the default) generates secrets of the form `ee<secret><hex(mask_host)>`. This is the DPI-resistant mode: the proxy masquerades as TLS traffic to the configured mask host, making it harder for deep-packet inspection to distinguish Telegram traffic from ordinary HTTPS.

**Random padding / Obfuscated2** (`random_padding = true`) generates secrets of the form `dd<secret>`, with no domain component. This is an older transport that is easier for DPI systems to fingerprint and is considered a DPI downgrade. Use it only if a specific client or network requires it.

Teleproxy serves both transports simultaneously. Toggling the setting only affects newly generated share links; existing client links continue to work. The `mask_host` value is not changed by this toggle.

### Switching share-link transport

1. Open the panel: **Settings → Proxy Settings**.
2. Check or uncheck **Padded (dd) links**.
3. Click **Save**. The panel re-renders `teleproxy.toml` and reloads Teleproxy.
4. Re-share each user's link or QR code from the Users section — the old links remain functional, but only the newly generated links reflect the new transport.

### Config file rendering

`mss_clamp` renders as a top-level key in `teleproxy.toml`:

```toml
mss_clamp = true
```

When `ja4_log` is enabled, a `[stats]` section is added:

```toml
[stats]
ja4_log = true
```

When `wildcard_mask` and `tls_backend` are both set, the domain entry uses the extended form:

```toml
domain = [{ name = "*.example.com", backend = "127.0.0.1:9443" }]
```

Without a TLS backend, the simple form is used:

```toml
domain = "proxy.example.com"
```

The DPI settings also become `ExecStart` flags on `tgproxy-panel.service`: `--mss-clamp=`, `--ja4-log=`, `--random-padding=`, and `--tls-backend` / `--wildcard-mask` when set.

## Updates

Manual update:

```bash
sudo tgproxy-cli update
```

Current behavior:

- checks GitHub Releases immediately for `tgproxy-cli`, `tgproxy-panel` (OstaninKI/MTProxyOrchestrator), `teleproxy` (teleproxy/teleproxy), and `sing-box` (SagerNet/sing-box)
- verifies SHA256 before replacing any binary; sing-box is distributed as a tar.gz archive and its binary is extracted after the archive is verified
- detects the installed version of `teleproxy` and `sing-box` by running the binary and parsing the semver from its version output (tries the `version` subcommand and `--version`)
- reconciles all config templates (systemd units, nginx, teleproxy.toml) with current config.toml and DB settings before restarting — this runs on every `update`, even when no new binaries are available, so template changes shipped without a version bump still reach disk
- rolls back on restart/health failure

### Config reconciliation

Before restarting services after an update, `tgproxy-cli update` re-renders all generated config files from the current `config.toml` and runtime DB settings. This ensures that new binaries receive up-to-date ExecStart flags, systemd hardening, and nginx security headers.

The reconciliation step:

1. reads `/etc/tgproxy/config.toml` (install-time settings)
2. reads runtime overrides from the `settings` table in `/etc/tgproxy/panel.db`
3. merges: DB overrides take precedence over config.toml values
4. re-renders `teleproxy.service`, `tgproxy-panel.service`, teleproxy.toml, and nginx configs to their canonical paths
5. renders `sing-box.service` only when Bridge mode is active
6. runs `systemctl daemon-reload` and `systemctl reload nginx`

All file writes use atomic rename (write to temp, rename) to avoid partial writes.

### nginx worker_connections tuning

The distro default `/etc/nginx/nginx.conf` ships with `worker_connections 768;` in the `events{}` block and no `worker_rlimit_nofile`. Under load — the public panel reverse proxy plus WebSocket log streaming — nginx exhausts that ceiling and floods `error.log` with:

```
[alert] ... 768 worker_connections are not enough
```

`worker_connections` is only valid in `events{}` and `worker_rlimit_nofile` only in the main context — neither can be set from a `sites-enabled/` or `conf.d/` drop-in (both are included inside `http{}`). So both install and reconcile patch the distro-owned `/etc/nginx/nginx.conf` in place (`internal/nginx.PatchMainConf`). The patch is idempotent and non-destructive:

- raises `worker_connections` to `4096` only when the current value is below it; an operator-set higher value is left untouched, and the directive is inserted into `events{}` if missing;
- raises `worker_rlimit_nofile` to `16384` only when below it, inserting it after `worker_processes` if missing.

Managed lines are tagged with a `# managed by tgproxy` marker. Because the patch only raises values below the floor, re-running it is a no-op: `nginx.conf` is not rewritten (both install and reconcile skip the write when the patched output is byte-identical). On reconcile this also means the patch triggers no nginx reload of its own; `install` always reloads nginx as part of provisioning regardless.

If you need different limits, set higher values manually in `/etc/nginx/nginx.conf` (the patch will not lower them) and run `sudo nginx -t && sudo systemctl reload nginx`.

**Important:** the reconciliation step runs with the **pre-update** (currently installed) CLI binary. This means that when a release changes the config template format or the version-detection logic itself, those new behaviors take effect only on the **next** reconciliation, executed by the freshly installed binary. When upgrading across such a release, run `sudo tgproxy-cli reconcile` once after the update completes — it regenerates every config with the new binary's templates. (Running `sudo tgproxy-cli update` a second time achieves the same, and saving the panel's Proxy Settings re-renders `teleproxy.toml` immediately.)

### Standalone reconcile

```bash
sudo tgproxy-cli reconcile
```

`reconcile` regenerates all generated configs (systemd units, nginx, teleproxy.toml, and sing-box.service in Bridge mode) from the installed binaries' templates and reloads the affected services — without checking GitHub or replacing any binary. Use it to apply template/config changes that ship without a version bump, for example after manually swapping in a new `tgproxy-panel` binary. It performs the same steps as the reconciliation phase of `update` (see above).

### SHA256 verification

SHA256 checksums are resolved in this order:

1. A `checksums.txt` or `SHA256SUMS` file attached to the GitHub release (parsed per asset name).
2. The `digest` field from the GitHub Releases API (format `sha256:<hex>`). This covers components like sing-box that do not publish separate checksum files.

If neither source provides a valid SHA256, the update is rejected (fail-closed).

sing-box is shipped as a tar.gz asset. `tgproxy-cli update` verifies the SHA256 of the downloaded archive before extracting the binary from it.

### Version format

Project releases use date-based version tags: `v<year>.<month>.<day>` with an optional fix suffix `-f<N>` for same-day patch releases. Examples: `v2026.5.10`, `v2026.5.10-f3`, `v2026.5.10-f12`. The update checker compares all four components, so `v2026.5.10-f3` is detected as an upgrade over `v2026.5.10-f2`.

The `--manual` flag currently defaults to `true`. Passing `--manual=false` enables the 18-hour throttling logic in the update checker.

## Uninstall

```bash
sudo tgproxy-cli uninstall
```

This removes managed binaries, systemd units, nginx stub config, and the tgproxy state directories.

## Smoke Test

The repository includes a post-install smoke script:

```bash
sudo bash scripts/smoke-ubuntu.sh
```

It verifies:

- Ubuntu version
- CLI availability
- `tgproxy-cli status`
- active systemd services
- nginx activity
- loopback panel health endpoint under the generated panel path
- nginx stub response

## Implementation Note

The interactive installer prompt layer is implemented through `github.com/charmbracelet/huh` behind an internal prompt abstraction.
