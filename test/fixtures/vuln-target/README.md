# vuln-target — deliberately-vulnerable probe validation fixture

⚠️ **TEST-ONLY. INTENTIONALLY VULNERABLE. DO NOT DEPLOY.**

This docker-compose fixture stands up a set of deliberately-weak HTTP surfaces so
chiller's OSS probes can be validated against **real positive findings** (#0d).
It is a *target that gets scanned* — it contains no chiller code and is not part
of the product image.

## Security posture (security-baseline)
- **No host ports are published.** Services are reachable only on the internal
  `vulnnet` bridge network, so the weak surfaces are never exposed to the host or
  LAN. A probe runner reaches them by joining `vulnnet` and using the service
  names `vulnweb` / `takeover-mock`.
- All "secret" surfaces (`.env`, `.git/config`, backup file) contain **dummy
  values only** — no real credentials or data.
- `testssl` (weak TLS, PS-002) is **not** in this fixture — it is validated
  against the public `badssl.com` test service instead.

## Surfaces → probe mapping
| Surface | Service / path | Probe · item | Expected finding |
|---|---|---|---|
| Missing HSTS / X-Frame-Options / CSP | `vulnweb` `/` | humble · PS-001/024 | CWE-319, CWE-1021 |
| Version banner `Server: nginx/<ver>` | `vulnweb` (`server_tokens on`) | httpx · PS-007/020 | CWE-693 |
| Wildcard CORS (`ACAO: *`) | `vulnweb` `/` | Corsy · PS-021 | CWE-346 |
| Directory listing (autoindex) | `vulnweb` `/listing/` | dirhunt · PS-004 | CWE-548 |
| Subdomain-takeover fingerprint body | `takeover-mock` `/` | subzy · PS-025 | (attacker axis) |

## Dormant observation surfaces (no finding today — by design)
`vulnweb` also serves `robots.txt`, `/.env`, `/.git/config`, `/crossdomain.xml`,
`/backup/db.sql.bak`, a sensitive HTML comment, an insecure `Set-Cookie`, and a
session-in-URL redirect (`/go`). These produce **no finding today**: the engine's
well-known-path GET provider is not wired yet (separate backlog). The surfaces are
seeded here so that increment can reuse this fixture directly.

## Run
```sh
docker compose -f test/fixtures/vuln-target/docker-compose.yml up -d
# ... run probes from a container joined to the vulnnet network ...
docker compose -f test/fixtures/vuln-target/docker-compose.yml down
```

Real tool output captured against this fixture is pinned as golden fixtures under
`internal/scan/checking/probe/testdata/`.
