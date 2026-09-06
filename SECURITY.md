# Security policy

Atrium runs inside a home LAN and holds family photos. The threat model assumes
the LAN is **not** trusted.

## Reporting a vulnerability

Open a private security advisory on the repository, or contact the maintainer
directly. Please do not file a public issue for an exploitable defect. Include
the version (`atrium version`), the configuration with paths redacted, and the
steps to reproduce.

## Security properties the project maintains

- No public port is opened and no UPnP/NAT traversal code exists.
- Every route except `/health/*`, the pairing endpoints and static assets
  requires a credential.
- Screen and admin credentials are separate. Screen credentials are `HttpOnly`,
  `SameSite=Strict`, `Secure` cookies (or a Bearer token); the admin credential
  is Bearer-only and is never accepted from a cookie.
- Only `sha256(token)` is stored; tokens never appear in URLs or logs.
- The NAS is opened read-only through an interface with no write methods.
  Symlinks are never followed and `..` is rejected.
- Previews are re-encoded, which strips EXIF and GPS data.
- Revocation closes live sessions immediately; clients drop cached data after
  24 hours offline.
- Pairing and authentication failures are rate limited.
- Backups exclude the preview cache and cannot target a source root.

## Operator responsibilities

- Keep `data_dir/admin.token` at mode `0600` and never copy it to a screen.
- Mount the NAS share with a read-only account; Atrium never stores NAS
  credentials.
- Use `tls.mode: off` only for a non-sensitive local demo.
