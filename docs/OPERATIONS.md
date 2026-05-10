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
- nginx stub site configuration
- `teleproxy.service`
- `tgproxy-panel.service`
- `/etc/tgproxy/teleproxy.toml`
- `/etc/tgproxy/secrets/users.json`
- `/etc/tgproxy/panel.db`

## Current Runtime Model

### Single mode

Current install flow provisions Single mode only.

```text
Telegram client -> Teleproxy :443 -> Telegram DC
Unknown probes/browser traffic -> nginx stub on loopback
```

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

### Remote access

The install path can publish the panel on a public TLS endpoint in two ways:

1. `--panel-domain`, `--panel-cert`, and `--panel-key` use operator-managed certificate files.
2. `--panel-domain` and `--panel-email` obtain a Let's Encrypt certificate during install and enable renewal from the panel service.

The panel backend remains plain HTTP on loopback (`127.0.0.1:18080`); nginx terminates public TLS on port `8443` when public panel flags are used.

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

If all recovery codes are also lost, recover with direct SQLite access on the host as a last resort:

```bash
sudo systemctl stop tgproxy-panel
sudo sqlite3 /etc/tgproxy/panel.db \
  "UPDATE admin SET totp_enabled = 0, totp_secret = '', totp_recovery_codes = '';"
sudo systemctl start tgproxy-panel
``` After recovery, log in with the password, re-enroll TOTP, and rotate the admin password using `tgproxy-cli reset-admin-password`. A dedicated `tgproxy-cli reset-totp` command is not yet implemented.

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

## Updates

Manual update:

```bash
sudo tgproxy-cli update
```

Current behavior:

- checks GitHub Releases immediately
- verifies SHA256 before replacing any binary
- rolls back on restart/health failure

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
