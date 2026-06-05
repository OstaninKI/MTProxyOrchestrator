# MTProto Proxy Orchestrator

MTProto Proxy Orchestrator manages a Teleproxy-based Telegram MTProto proxy on Ubuntu 22.04+.

## Current Scope

- Installs a Single-mode Teleproxy deployment with nginx stub fallback and systemd units
- Routes invalid FakeTLS probes to a loopback HTTPS stub backend when panel TLS is configured, so browser probes see the deployed domain certificate instead of the external mask host certificate
- Runs an authenticated admin panel backend on loopback and can wire a public HTTPS nginx proxy when certificate paths are provided
- Supports user management, Bridge runtime switching, metrics, logs, backups, restore, and verified updates in the codebase
- Enforces optional per-user traffic quotas with daily, weekly, or monthly periods, soft warning, and hard suspension
- Supports optional TOTP-based 2FA per admin account with single-use bcrypt-hashed recovery codes
- Exposes DPI-resistance controls in the panel under Settings → Proxy Settings: MSS clamp, JA4 fingerprint logging, Fake-TLS vs random-padding link transport, and optional TLS backend / wildcard masquerade domain
- Publishes and consumes release assets from `github.com/mtproto-orchestrator/mtproto-orchestrator`

## Important Current Limitation

The installer provisions Single mode first. Bridge mode is enabled later from the authenticated panel. The installer can publish the admin panel through nginx either with operator-provided certificate files or by obtaining a Let's Encrypt certificate when `--panel-domain` and `--panel-email` are provided.

## Requirements

- Ubuntu 22.04 or later
- Root privileges
- Ports `80` and `443` available for nginx/Teleproxy
- Port `8443` available for the public HTTPS admin panel when panel TLS flags are used
- Loopback port `9443` available for the internal HTTPS stub backend when panel TLS flags are used
- Outbound HTTPS access to GitHub Releases

## Install

Interactive install:

```bash
sudo tgproxy-cli install
```

Current interactive prompts are limited to:

- FakeTLS mask host
- Final confirmation for a Single-mode install

When panel TLS is configured, the installer uses the panel domain as the FakeTLS hostname for generated user links and points Teleproxy fallback traffic at `panel-domain:9443`. nginx serves that backend on loopback with the same certificate, while the public admin panel remains on `8443`.

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

## Local Development (Dev Mode)

Run the panel locally without a Linux server or any installed services:

```bash
go run ./cmd/tgproxy-panel/ serve --dev
```

The panel starts at **http://127.0.0.1:8080/login**. Log in with `admin` / `admin`.

What dev mode does:
- Uses an in-memory SQLite database (no files written)
- Seeds demo data: 5 users, traffic history, settings
- Stubs all OS side-effects: no systemd calls, no file writes to `/etc/tgproxy/`, no ACME renewal
- Sets `Secure=false` so session cookies work over plain HTTP
- Defaults to `127.0.0.1:8080` and path `/`

Override defaults if needed:

```bash
go run ./cmd/tgproxy-panel/ serve --dev --listen 0.0.0.0:9000 --path /p-dev/
```

## Common Commands

```bash
tgproxy-cli install
tgproxy-cli install --unattended
tgproxy-cli status
tgproxy-cli update
tgproxy-cli reconcile
tgproxy-cli reset-admin-password
tgproxy-cli reset-totp
tgproxy-cli backup --dest /path/to/backup.enc --passphrase 'secret'
tgproxy-cli restore /path/to/backup.enc --passphrase 'secret'
tgproxy-cli uninstall
```

## Updates

- All four components are version-checked against GitHub Releases: tgproxy-cli, tgproxy-panel (OstaninKI/MTProxyOrchestrator), teleproxy (teleproxy/teleproxy), and sing-box (SagerNet/sing-box)
- Binary downloads are verified by SHA256 before replacement; sing-box ships as a tar.gz asset and its binary is extracted from the verified archive
- Failed service restart or health check triggers rollback from the backup binary

## Docs

- [Operations](docs/OPERATIONS.md)
- [Security](docs/SECURITY.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
