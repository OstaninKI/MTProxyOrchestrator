# MTProto Proxy Orchestrator: Technical Specification

## 1. Goal

Build `MTProto Proxy Orchestrator` for Ubuntu 22.04+ that deploys an MTProto proxy for Telegram with DPI resistance comparable to normal Chrome HTTPS traffic, realistic probe fallback to a stub website, and a web administration panel.

The system is intended for a closed group of trusted users, roughly up to 16 concurrent users, accounting for Teleproxy's secret limit.

## 2. Operating Modes

### Single

Single is the default mode. A Telegram client connects to `Teleproxy` on port 443 through FakeTLS, and `Teleproxy` connects directly to Telegram DCs.

Key properties:

- `sing-box` is not installed.
- Fewer moving parts and lower risk of operational mistakes.
- Suitable as the starting and main mode for most scenarios.

### Bridge

Bridge is optional. A local `sing-box` instance is inserted between `Teleproxy` and Telegram DCs and sends traffic through one or more external outbound nodes.

Key properties:

- `Teleproxy` sends traffic to a local SOCKS5 `sing-box` listener.
- `sing-box` routes traffic through VLESS Reality, Trojan, Shadowsocks-2022, Hysteria2, or TUIC v5.
- Switching `Single -> Bridge` and `Bridge -> Single` must be possible from the panel without reinstalling.

## 3. Architecture

### Single

```text
TG client
  -> FakeTLS / Chrome-like TLS 1.3
  -> Teleproxy :443
  -> Telegram DC

Unknown probes / browsers
  -> Teleproxy probe fallback
  -> nginx 127.0.0.1
  -> static stub site
```

### Bridge

```text
TG client
  -> FakeTLS / Chrome-like TLS 1.3
  -> Teleproxy :443
  -> SOCKS5 loopback
  -> sing-box router
  -> outbound node
  -> Telegram DC

Unknown probes / browsers
  -> Teleproxy probe fallback
  -> nginx 127.0.0.1
  -> static stub site
```

## 4. Components

| Component | Role | Source |
| --- | --- | --- |
| Teleproxy | MTProto proxy with FakeTLS, DRS, and probe resistance | `github.com/teleproxy/teleproxy` |
| sing-box | Multi-outbound router used only in Bridge mode | `github.com/SagerNet/sing-box` |
| nginx | Loopback stub website and panel TLS | System package |
| tgproxy-cli | Installer, configurator, backup/restore/update tool | This project |
| tgproxy-panel | Web panel, metrics, user and node management | This project |

## 5. tgproxy-cli

`tgproxy-cli` is distributed as a single Go binary. It relies on standard Unix utilities on the host and system `systemd`.

### Commands

```bash
tgproxy-cli install
tgproxy-cli install --unattended
tgproxy-cli update
tgproxy-cli uninstall
tgproxy-cli status
tgproxy-cli reset-admin-password
tgproxy-cli backup
tgproxy-cli restore <archive>
```

Backups include only configuration and state required to restore `/etc/tgproxy/` and must be encrypted because they contain operational secrets.

### Installation Wizard

1. Check Ubuntu 22.04+, root privileges, systemd, free ports 443 and panel port, RAM, and disk space.
2. Install `nginx`, `curl`, and `ca-certificates`.
3. Ask for a domain or IP address.
4. For a domain, verify the A record and offer Let's Encrypt.
5. For an IP address, issue a self-signed panel certificate.
6. Configure `Teleproxy`: mask host, port, logging, keepalive.
7. Select `Single` or `Bridge`, defaulting to `Single`.
8. For `Bridge`, ask for at least one outbound node and routing strategy.
9. Generate a random panel path, admin login, and admin password.
10. Download Teleproxy binaries and, when required, sing-box binaries from GitHub Releases with SHA256 verification.
11. Create configs, directories, permissions, and systemd units.
12. Start services and run a health check.
13. Print the panel URL, login, password, and first `tg://` link.

### Defaults

| Parameter | Default |
| --- | --- |
| MTProto port | `443` |
| Mask host | `www.microsoft.com` |
| Mode | `Single` |
| Bridge strategy | `urltest`, 30 seconds |
| Log level | `info` |
| Panel path | `/p-{8 random chars}/` |
| TCP keepalive | 60 seconds |

## 6. Server File Structure

```text
/etc/tgproxy/
  config.toml
  teleproxy.toml
  sing-box.json
  secrets/users.json
  nodes/outbounds.json
  stub-templates/
  panel.db

/var/log/tgproxy/
  panel.log
  teleproxy.log
  sing-box.log
  nginx.log

/usr/local/bin/
  tgproxy-cli
  tgproxy-panel
  teleproxy
  sing-box

/etc/systemd/system/
  teleproxy.service
  sing-box.service
  tgproxy-panel.service

/var/www/tgproxy-stub/
  index.html
  css/
  assets/
```

`sing-box.json`, `sing-box`, `sing-box.service`, `nodes/outbounds.json`, and `sing-box.log` are required only in Bridge mode.

## 7. tgproxy-panel

### Stack

- Go 1.22+
- `net/http` + `chi`
- `html/template` + `embed.FS`
- HTMX + AlpineJS + Tailwind CSS without SPA build tooling
- SQLite through `modernc.org/sqlite`
- WebSocket through `gorilla/websocket`
- ACME through `github.com/go-acme/lego`
- CLI through `spf13/cobra` and an actively maintained prompt library behind an internal prompt interface
- Prometheus parsing through `prometheus/common/expfmt`
- Logs through `log/slog`

### Authentication

- HTTPS is mandatory.
- The panel is available through a random path such as `/p-a8f3k2x9/`.
- The login is generated during installation: 8 characters, lower-case letters and digits.
- The password is generated automatically, minimum 16 characters.
- Manually changed passwords must contain at least 16 characters, letters, and digits.
- Password hashing uses bcrypt cost >= 12.
- Sessions use `HttpOnly`, `Secure`, `SameSite=Strict` cookies.
- A CSRF token is required in every POST form.
- Rate limit: 5 failed attempts per IP in 5 minutes, then block for 1 hour.
- All admin actions are written to the SQLite audit log.
- Optional TOTP-based two-factor authentication is available per admin account. When enabled, password verification is followed by a `/totp/verify` step backed by a short-lived (5 minutes) pending-TOTP cookie. The same login rate limiter covers failed TOTP attempts. Eight single-use recovery codes are generated at enrollment and stored as bcrypt hashes (cost >= 12); regenerating recovery codes and disabling TOTP both require a valid TOTP or recovery code. Audit events: `totp_enabled`, `totp_disabled`, `totp_recovery_regenerated`, `totp_recovery_used`, `totp_failed`.

### Panel Sections

#### Dashboard

- Status of `Teleproxy`, `sing-box`, and `nginx`.
- Active connections per user.
- Component versions and available update indicator.
- Top 5 users by traffic.
- Periods: 1 hour, 24 hours, 7 days, 30 days.

#### Users

Features:

- user list;
- create a user with label `a-z`, `0-9`, `_`, up to 32 characters;
- automatic secret generation;
- provide a `tg://proxy?...` link and QR code;
- enable and disable a user;
- soft-delete;
- rotate a key with immediate invalidation of the old secret;
- audit operations without writing secrets to the log.

#### Per-User Traffic Quotas

Optional per-user traffic quotas are enforced by the panel. Each user record carries:

- `quota_bytes` (zero means unlimited);
- `quota_period`, one of `daily`, `weekly`, `monthly`;
- `quota_warn_pct` (default `80`);
- `quota_period_start`, `quota_used_bytes`, `quota_warned`, `quota_suspended`.

Behavior:

- A background service iterates users every 5 minutes, performs period rollover when due, and recomputes `quota_used_bytes` from `traffic_daily` since `quota_period_start`.
- Crossing `quota_warn_pct` writes a single `quota_warning` audit event per period.
- Crossing `quota_bytes` sets `quota_suspended = 1`; suspended users are excluded from the rendered Teleproxy config and the service is reloaded only on transitions.
- The panel exposes per-user actions: set quota, reset quota counters, toggle suspension. All actions are CSRF-protected and audited (`quota_set`, `quota_reset`, `user_suspend_toggle`).
- Period math: `daily` rolls every 24 hours, `weekly` every 7 days, `monthly` advances one calendar month using `time.Date`.

#### Bridge / Outbound Nodes

Features:

- show the current mode: `Single` or `Bridge`;
- wizard to enable Bridge with the first node;
- disable Bridge and return to direct mode;
- outbound node list;
- add, edit, delete, latency test, and temporarily disable nodes;
- import share URLs: `vless://`, `trojan://`, `ss://`, `hysteria2://`, `tuic://`.

Supported protocols:

- VLESS + Reality;
- Trojan;
- Shadowsocks-2022;
- Hysteria2;
- TUIC v5.

Strategies:

- `urltest`;
- `round-robin`;
- `fallback`;
- `selector`.

#### Stub Templates

Built-in templates:

- Maintenance;
- Coming Soon;
- Personal Blog;
- Corporate Landing;
- Dev Portfolio.

Requirements:

- only HTML, CSS, images, fonts, and SVG;
- no external requests;
- no JavaScript dependencies;
- responsive layout;
- 5-30 KB for built-in templates;
- custom ZIP up to 5 MB with content validation.

#### Logs And Debug

- Live stream through WebSocket.
- Filter by component: `panel`, `teleproxy`, `sing-box`, `nginx`.
- Filter by level: `error`, `warn`, `info`, `debug`.
- Text search.
- Download the last N lines.
- Test the chain to Telegram with stages and latency.

#### SSL / Certificates

For a domain:

- view current certificate;
- manual renewal;
- automatic renewal 30 days before expiry;
- renewal attempt log.

Without a domain:

- show self-signed certificate status;
- explain that Let's Encrypt is unavailable without a domain.

#### Settings

- mask host;
- MTProto port;
- global limits;
- panel path change;
- admin password change;
- log level;
- metrics retention period.

#### Updates

- GitHub Releases API is checked automatically no more than once every 18 hours.
- Manual checks are available without a rate limit.
- Component update flow: download from GitHub Releases, SHA256, backup, atomic move, restart, health-check, rollback.
- Update history and rollback support.

## 8. Metrics

Sources:

- Teleproxy per-secret Prometheus endpoint;
- sing-box per-outbound metrics.

Base table:

```sql
CREATE TABLE traffic_samples (
    id          INTEGER PRIMARY KEY,
    user_label  TEXT NOT NULL,
    ts          INTEGER NOT NULL,
    bytes_in    INTEGER NOT NULL,
    bytes_out   INTEGER NOT NULL,
    connections INTEGER NOT NULL
);
CREATE INDEX idx_user_time ON traffic_samples(user_label, ts);
```

Sampling runs every 60 seconds.

Retention:

- keep minute-level data for up to 7 days;
- aggregate data older than 7 days into hourly rows;
- delete data older than 30 days or aggregate it into daily rows if enabled in settings.

## 9. Security

- Configs and secrets: owner `root`, mode `600`.
- systemd: `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`, `AmbientCapabilities=CAP_NET_BIND_SERVICE`, `DynamicUser=yes` where applicable.
- nginx: TLS 1.2/1.3, strong cipher suites, HSTS, `server_tokens off`.
- Logs: daily logrotate, 14-day retention, no secret logging.
- The panel does not execute arbitrary shell commands based on user input.
- Updates run only after SHA256 verification and support rollback.

## 10. Development Stages

| Stage | Contents | Completion Criteria |
| --- | --- | --- |
| MVP-1 | CLI installer deploys Teleproxy and nginx stub in Single mode | Telegram connects through FakeTLS, probes see the stub |
| MVP-2 | Minimal panel: auth, list/add/delete/rotate users | Users are managed through the web |
| MVP-3 | Bridge: sing-box, first node, mode switching | Traffic goes through outbound, modes switch without reinstall |
| MVP-4 | Multi-node and routing strategies | 2+ nodes work with the selected strategy |
| MVP-5 | Metrics, counters, periods | Per-user reports work |
| MVP-6 | Stub templates and Let's Encrypt | Certificates renew, templates can be switched |
| MVP-7 | Component updates and live logs | Updates with rollback and live logs work |

## 11. Out Of Scope For v1

- Managing remote outbound nodes.
- Billing.
- Multi-tenant and administrator roles.
- Public API.
- Mobile admin application.
- Clustering and high availability.
- Full OS support beyond Ubuntu 22.04+; Debian 12 is best effort only.

## 12. v2 Preparation: Telegram Bot

In v2, a separate `tgproxy-bot` service is possible:

- token stored in `/etc/tgproxy/config.toml`;
- access only through a Telegram User ID whitelist;
- communication with the panel through a Unix socket or loopback API;
- commands: `/status`, `/users`, `/add`, `/disable`, `/enable`, `/rotate`, `/delete`, `/nodes`, `/logs`, `/updates`;
- push notifications about new versions, service failures, and anomalous traffic.

For v1, the following should be prepared:

- internal loopback-only API;
- user and node structures without future breaking migrations;
- commented `[telegram_bot]` section in the config.

## 13. Open Questions

| Question | Deadline |
| --- | --- |
| License: Apache 2.0 | Resolved |
| Final project name and domain | Before MVP-1 |
| Binary hosting: GitHub Releases | Resolved |
| User communication channel | Before public release |
| Real probe resistance of Bridge configurations | Separate traffic audit |

## 14. Technical Notes

- Teleproxy is limited to 16 secrets; an `MTProto engine` abstraction should be prepared.
- Chrome DRS emulation is not permanent; Teleproxy updates are critical.
- `urltest` creates additional outbound HTTPS traffic.
- Changing the mask host requires reissuing `tg://` links for all users.
- A self-signed panel certificate is expected to trigger a browser warning.
