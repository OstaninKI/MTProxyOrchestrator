# Security

## Threat Model Summary

MTProto Proxy Orchestrator is a single-host, single-admin system intended for trusted operators. The current implementation primarily defends against:

- unauthorized access to the admin panel backend
- accidental disclosure of MTProto secrets, admin credentials, and backup material
- unsafe restore input that attempts path traversal or oversized extraction
- unverified binary replacement during updates
- service compromise through overly broad systemd privileges

Out of scope for v1:

- public multi-tenant administration
- public HTTP API
- host compromise outside this application boundary
- network-layer DDoS mitigation

## Secret Handling

### Stored secrets and sensitive state

| Item | Current storage |
| --- | --- |
| Admin login + bcrypt hash | `/etc/tgproxy/panel.db` |
| MTProto user secrets | `/etc/tgproxy/secrets/users.json` and panel DB user records |
| Teleproxy runtime config | `/etc/tgproxy/teleproxy.toml` |
| Bridge node config | `/etc/tgproxy/nodes/outbounds.json` and `/etc/tgproxy/sing-box.json` when Bridge is active |
| TLS private keys | under the configured certificate directory, written with `0600` |
| Update checker state | files under `/etc/tgproxy/` |

### File permissions

- Sensitive files under `/etc/tgproxy/` are expected to be owned by `root` and written with mode `0600`.
- The config directory and its private subdirectories are created with root-only permissions.
- `teleproxy.service` runs with a dedicated `teleproxy` system user created during install. The unit keeps only the capabilities needed to bind port 443 and let Teleproxy perform its internal user/group switch.
- `tgproxy-panel.service` does **not** use `DynamicUser=yes` because the current storage model requires writes to root-owned state in `/etc/tgproxy/`.
- The panel systemd unit now sets `UMask=0077` so new state files are not created group/world-readable.

## Authentication and Session Controls

- Password hashing uses bcrypt with cost `12`.
- Session cookies are `HttpOnly`, `Secure`, and `SameSite=Strict`.
- Every POST form is protected by CSRF validation.
- Failed login attempts are rate-limited to 5 per IP per 5 minutes, then blocked for 1 hour.
- When the panel is behind local nginx, the app trusts the loopback proxy's `X-Real-IP` value instead of a client-appended `X-Forwarded-For` chain.
- Audit rows record admin actions without raw secrets or session tokens.

### Optional Two-Factor Authentication

- TOTP-based 2FA is opt-in per admin account. Default state is disabled, preserving prior login behavior for existing deployments.
- After password verification, the panel issues a 5-minute pending-TOTP cookie and redirects to `/totp/verify`. The session cookie is only minted after the second factor is verified.
- Failed TOTP attempts are counted by the same login rate limiter (5 per IP per 5 minutes, then 1 hour block).
- Eight single-use recovery codes are generated at enrollment and stored as bcrypt hashes (cost `12`). Recovery codes are removed from storage on first use.
- Disabling TOTP and regenerating recovery codes both require a current TOTP code or an unused recovery code. State changes are logged: `totp_enabled`, `totp_disabled`, `totp_recovery_regenerated`, `totp_recovery_used`, `totp_failed`.
- Lost 2FA device recovery is documented in `docs/OPERATIONS.md`; if recovery codes are also lost, a host-level SQLite reset is the documented last resort.

## Quota-Based Abuse Mitigation

- Per-user traffic quotas are optional. When configured, exceeding the limit suspends the user automatically and excludes their secret from the rendered Teleproxy config until the next period rollover or a manual reset.
- The quota service crosses the warn threshold once per period and writes a single `quota_warning` audit event, avoiding noisy repeats.
- Suspension transitions trigger a Teleproxy reload only on state changes, so a steady-state quota check does not perturb the running service.
- This control is intended as a guard against a leaked user secret consuming the channel, not as a billing mechanism.

## Backup Handling

- Backups include configuration/state needed to restore `/etc/tgproxy/`.
- Backup archives are encrypted before being written to disk.
- Backup output files are created with mode `0600`.
- Restore validates archive entry paths and rejects absolute paths, `..`, and other escape attempts.
- Restore enforces limits on archive size, file count, and per-file extraction size.
- Custom stub ZIP uploads are size-limited before multipart form parsing and enforce file count, per-file, total extracted-size, path traversal, and symlink checks.

## Update Verification and Rollback

1. Update metadata is selected from GitHub Releases.
2. The exact selected asset must have a matching SHA256 entry from the release checksum data.
3. The candidate binary is downloaded to a temporary path and verified before replacement.
4. The existing binary is backed up.
5. The new binary replaces the old one only after checksum verification succeeds.
6. The affected service is restarted and health-checked.
7. If restart or health check fails, the backup binary is restored.

## Logging and Audit Expectations

- Raw admin passwords must not be written to logs or audit rows.
- Raw MTProto secrets must not be written to logs or audit rows.
- Session identifiers must not be written to audit rows.
- Operator-visible logs are component-scoped (`panel`, `teleproxy`, `sing-box`, `nginx`) and should be treated as potentially sensitive operational data.
- Teleproxy metric scraping and GitHub update metadata checks use explicit HTTP client timeouts; metric responses are size-capped before parsing.
- Dashboard Teleproxy observability is available only on authenticated panel routes. JA4 fingerprint hashes, per-secret rejection counters, connection/IP limits, and SOCKS5 upstream counters are operational telemetry and must not be exposed through public unauthenticated endpoints.

## DPI Resistance

Generated client links default to Fake-TLS transport (the `ee`-prefix secret with a masquerade domain). MSS clamp and JA4 fingerprint logging are both enabled by default, providing fragmentation of ClientHello across TCP segments and per-connection fingerprint visibility on Teleproxy's `/stats` and `/metrics` endpoints.

Operators may switch a proxy to random-padding (Obfuscated2, `dd`-prefix secret) links via Settings → Proxy Settings. This is an explicit DPI-resistance downgrade relative to Fake-TLS: Obfuscated2 traffic is older, lacks a TLS masquerade domain, and is easier to fingerprint by network-layer DPI. It should be a deliberate, intentional choice.

Teleproxy serves both transports simultaneously. Toggling `random_padding` only changes newly generated share links; previously distributed links continue to function without interruption.

**Known limitation**: the Telegram client's own TLS fingerprint is negotiated client-side and cannot be altered or fixed server-side.

## Known Non-Goals

- No multi-tenant admin roles
- No public API
