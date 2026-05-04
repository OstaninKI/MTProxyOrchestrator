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
