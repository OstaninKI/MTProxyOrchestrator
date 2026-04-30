# MTProto Proxy Orchestrator (tgproxy)

Deploys and manages a [Teleproxy](https://github.com/Kkevsterrr/teleproxy)-based Telegram MTProto proxy on Ubuntu 22.04+.

## What it does

- Installs and configures Teleproxy, nginx, and (optionally) sing-box on a single server
- Provides a HTTPS admin panel for managing users, proxy modes, certificates, logs, and updates
- Handles certificate issuance and renewal via ACME (Let's Encrypt)
- Supports encrypted backup and restore of all configuration

## Modes

| Mode | Description |
|---|---|
| **Single** (default) | Teleproxy listens on port 443 and connects directly to Telegram data centres. sing-box is not installed. |
| **Bridge** | Teleproxy forwards traffic through a local sing-box SOCKS5 listener to one or more outbound relay nodes. |

## Requirements

- Ubuntu 22.04 or later (64-bit)
- Root access
- Ports 80 and 443 available

## Quick Start

Download the latest `tgproxy-cli` binary from [GitHub Releases](https://github.com/mtproto-orchestrator/mtproto-orchestrator/releases), then:

```bash
sudo tgproxy-cli install
```

The installer walks through domain, mode, and admin password setup. The admin panel is available at `https://your-domain` once installation completes.

### Unattended install

```bash
sudo tgproxy-cli install \
  --domain proxy.example.com \
  --mode single \
  --admin-password 'ChangeMe!'
```

## Binaries

Pre-built binaries for Linux/amd64 are published on [GitHub Releases](https://github.com/mtproto-orchestrator/mtproto-orchestrator/releases).

Each release includes SHA256 checksums. `tgproxy-cli update` verifies checksums automatically before replacing binaries.

## Common Commands

```bash
tgproxy-cli status                  # Show service status
tgproxy-cli update                  # Update to latest release
tgproxy-cli backup --dest /path/to/backup.enc --passphrase 'XXX'
tgproxy-cli restore /path/to/backup.enc --passphrase 'XXX'
tgproxy-cli reset-admin-password    # Reset the panel admin password
tgproxy-cli uninstall               # Remove everything
```

## License

Apache License 2.0 — see [LICENSE](LICENSE).
