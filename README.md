# MTProto Proxy Orchestrator

MTProto Proxy Orchestrator manages a Teleproxy-based Telegram MTProto proxy on Ubuntu 22.04+.

## Current Scope

- Installs a Single-mode Teleproxy deployment with nginx stub fallback and systemd units
- Runs an authenticated admin panel backend on loopback and can wire a public HTTPS nginx proxy when certificate paths are provided
- Supports user management, Bridge runtime switching, metrics, logs, backups, restore, and verified updates in the codebase
- Enforces optional per-user traffic quotas with daily, weekly, or monthly periods, soft warning, and hard suspension
- Supports optional TOTP-based 2FA per admin account with single-use bcrypt-hashed recovery codes
- Publishes and consumes release assets from `github.com/mtproto-orchestrator/mtproto-orchestrator`

## Important Current Limitation

The installer provisions Single mode first. Bridge mode is enabled later from the authenticated panel. The installer can publish the admin panel through nginx either with operator-provided certificate files or by obtaining a Let's Encrypt certificate when `--panel-domain` and `--panel-email` are provided.

## Requirements

- Ubuntu 22.04 or later
- Root privileges
- Ports `80` and `443` available for nginx/Teleproxy
- Port `8443` available for the public HTTPS admin panel when panel TLS flags are used
- Outbound HTTPS access to GitHub Releases

## Install

Interactive install:

```bash
sudo tgproxy-cli install
```

Current interactive prompts are limited to:

- FakeTLS mask host
- Final confirmation for a Single-mode install

Unattended install:

```bash
sudo tgproxy-cli install --unattended
```

Unattended install with a public HTTPS admin panel proxy:

```bash
sudo tgproxy-cli install --unattended \
  --panel-domain proxy.example.com \
  --panel-cert /etc/tgproxy/certs/proxy.example.com/cert.pem \
  --panel-key /etc/tgproxy/certs/proxy.example.com/key.pem
```

Unattended install with Let's Encrypt for the public HTTPS admin panel proxy:

```bash
sudo tgproxy-cli install --unattended \
  --panel-domain proxy.example.com \
  --panel-email admin@example.com
```

The installer generates:

- random panel path
- random admin login
- random admin password
- first MTProto user secret

At the end it prints the panel path, admin credentials, and the first `tg://proxy` link.

## Runtime Layout

Key paths used by the current implementation:

- `/etc/tgproxy/teleproxy.toml`
- `/etc/tgproxy/secrets/users.json`
- `/etc/tgproxy/panel.db`
- `/etc/systemd/system/teleproxy.service`
- `/etc/systemd/system/tgproxy-panel.service`
- `/etc/nginx/sites-available/tgproxy-stub`
- `/etc/nginx/sites-available/tgproxy-stub-tls` when panel TLS flags are used
- `/etc/nginx/sites-available/tgproxy-panel` when panel TLS flags are used

## Admin Panel Backend

- Backend listen address: `127.0.0.1:18080`
- Public HTTPS listen address: `0.0.0.0:8443` when panel TLS flags are used
- Protocol: plain HTTP on loopback
- Health endpoint: `http://127.0.0.1:18080/<generated-panel-path>/health`
- Authenticated UI: mounted under the generated random path only
- Dashboard navigation and subpage back links preserve the generated panel path.

## Common Commands

```bash
tgproxy-cli install
tgproxy-cli install --unattended
tgproxy-cli status
tgproxy-cli update
tgproxy-cli reset-admin-password
tgproxy-cli reset-totp
tgproxy-cli backup --dest /path/to/backup.enc --passphrase 'secret'
tgproxy-cli restore /path/to/backup.enc --passphrase 'secret'
tgproxy-cli uninstall
```

## Updates

- Binary downloads are verified by SHA256 before replacement
- Failed service restart or health check triggers rollback from the backup binary
- Release asset selection currently targets the GitHub Releases repository above

## Docs

- [Operations](docs/OPERATIONS.md)
- [Security](docs/SECURITY.md)
- [Technical specification](docs/TECHNICAL_SPEC.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
