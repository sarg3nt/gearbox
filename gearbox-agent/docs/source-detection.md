# Source detection

The gearbox-agent probes every supported "source" (HAProxy, nginx, Apache, Caddy, Traefik, Docker, plus the always-present `host` entry) at startup and reports each one's status to the dashboard via `GET /api/v1/system/capabilities`. The dashboard uses that manifest to decide which source cards to render, which gears to hide, and — once multiple HTTP producers exist on the same host — which one's data to display by default.

Auto-detection covers the common case with no configuration. The override env vars below exist for the rare edge cases. This doc walks the probe precedence model, the per-source troubleshooting recipes, and the limitations to be aware of.

## Probe precedence (per source)

Each detector runs the same four-step decision tree at startup:

```text
1. Explicit override URL configured (e.g. NGINX_STATUS_URL)?
        ├── Yes → Available, trust operator, no synchronous probe.
        └── No  → 2

2. Binary on PATH (e.g. exec.LookPath("nginx"))?
        ├── No  → not_installed (the source isn't here at all)
        └── Yes → 3

3. Default status / metrics endpoint reachable?
        ├── 200 + sentinel body match → Available
        ├── 403                       → Inaccessible (perms)
        ├── 404                       → Inaccessible (surface not configured)
        ├── 200 without sentinel      → Inaccessible (catch-all vhost)
        └── connection refused / timeout → Inaccessible (no listener)

4. Operator override via GEARBOX_AGENT_HTTP_SOURCE selects this gear?
        └── Same Available/fall-back logic as above (see "Metric-source
            overrides" in the agent README).
```

The detector's verdict lands in the capability manifest with a free-text `reason` field aimed at operators — not just the four-state enum. If the status is `inaccessible`, the reason names the surface that was probed and the snippet that would fix it (e.g. for nginx 403, "add `allow 127.0.0.1; deny all;` to the stub_status location").

`Probe()` is cheap and bounded. The HTTP probe used by the web-server detectors has a 1-second timeout; binary lookups are pure `exec.LookPath`; config-path heuristics are `os.Stat` only. A misbehaving local service can't stall agent startup.

## Per-source troubleshooting

### nginx — "installed but capability shows inaccessible"

The probe expects `http://127.0.0.1/nginx_status` to return 200 with a body that starts with `Active connections:`. Add a stub_status location to one of your server blocks:

```nginx
server {
    listen 127.0.0.1:80;
    server_name localhost;

    location /nginx_status {
        stub_status;
        allow 127.0.0.1;
        deny all;
    }
}
```

If the probe gets 403, the location is there but the `allow` rule blocked us — usually because nginx is fronted by Cloudflare's `set_real_ip_from` and `127.0.0.1` is rewritten before the `allow` check runs. Either add `real_ip_header X-Real-IP;` before the `allow`, or set `NGINX_STATUS_URL` to whatever surface the agent can actually reach.

If you run nginx Plus or open-source 1.19+ with `--with-http_api_module`, the detector records `api_module=true` in the manifest. Phase 4's metrics gear will prefer the JSON API over `stub_status` automatically; no action needed.

### Apache — "installed but capability shows inaccessible"

The probe expects `http://127.0.0.1/server-status?auto` to return 200 with a body that starts with `Total Accesses:`. Two changes are typical:

1. Load `mod_status`. Debian/Ubuntu: `sudo a2enmod status`. RHEL/Fedora: it's loaded by default, check `httpd -M | grep status_module`.

2. Add a Location block (Debian/Ubuntu defaults already include one; RHEL doesn't):

   ```apache
   <Location "/server-status">
       SetHandler server-status
       Require local
   </Location>
   ```

If the probe gets 403, the location is there but the `Require local` directive (or equivalent `Require ip 127.0.0.1`) is missing or refers to a different IP than the loopback the agent is probing from.

The detector tries `apache2` (Debian/Ubuntu) first, then falls back to `httpd` (RHEL/Fedora). The `binary` capability key records which one was found.

### Caddy — "installed but capability shows inaccessible"

Caddy's admin endpoint is on by default at `:2019` and exposes Prometheus at `:2019/metrics`. The probe expects 200 with a body containing `caddy_http_requests_total`.

The most common reason this fails: someone added `admin off` to their Caddyfile. To re-enable just the admin endpoint without the dashboard:

```caddyfile
{
    admin :2019 {
        origins 127.0.0.1
    }
}
```

If you can't or won't expose the admin endpoint, set `CADDY_ADMIN_URL` to wherever your Prometheus exporter actually lives — the agent will trust the operator and skip the synchronous probe.

### Traefik — "installed but capability shows inaccessible"

Traefik's Prometheus surface is opt-in. The detector tries `:8082/metrics` first (the conventional metrics entrypoint), then falls back to `:8080/metrics` (the dashboard API entrypoint). Both have to be unreachable for the verdict to be `inaccessible`.

Enable Prometheus in your static config:

```yaml
# traefik.yml
metrics:
  prometheus:
    entryPoint: metrics

entryPoints:
  metrics:
    address: ":8082"
```

If your metrics live somewhere non-default (a different port, an `/internal/metrics` path, behind a basic-auth middleware), set `TRAEFIK_METRICS_URL` to whatever the agent can reach.

### Docker — "installed but capability shows inaccessible"

The detector finds the docker binary on PATH but can't `os.Stat` the socket. Common causes:

- `dockerd` isn't running. `sudo systemctl start docker` — the `service_active` field in the manifest will go from `false` to `true` after agent restart.
- Container-mode agent and the socket isn't bind-mounted. Add `-v /var/run/docker.sock:/var/run/docker.sock` to the agent's docker-compose definition.
- Rootless docker. The socket is in the user's home (`~/.docker/run/docker.sock`). Set `DOCKER_SOCKET=/home/<user>/.docker/run/docker.sock`.

## Capability map keys (per source)

The detector populates the `ProbeResult.Capabilities` map with the facts it discovered. Stable keys across sources where applicable:

| Key                 | Example                              | Notes                                                                  |
|---------------------|--------------------------------------|------------------------------------------------------------------------|
| `version`           | `1.27.0`                             | Parsed from `--version` / `-v` output.                                  |
| `binary_path`       | `/usr/sbin/nginx`                    | Result of `exec.LookPath`.                                              |
| `config_path`       | `/etc/nginx/nginx.conf`              | Either auto-detected or from env var.                                   |
| `status_url`        | `http://127.0.0.1/nginx_status`      | URL the agent successfully probed (or the configured one).              |
| `status_source`     | `stub_status` / `mod_status` / `prometheus` | Which mechanism the agent will use to read metrics in Phase 4+.  |
| `override_source`   | `env`                                | Set when an env var influenced the verdict — easier to spot at a glance. |

Source-specific keys exist too — `api_module` for nginx, `status_module` for Apache, `dashboard_api` for Traefik, `socket_path` and `service_active` for Docker.

## Conflict-resolution semantics

The "multiple potentially-conflicting resources on one box" scenario splits into three flavours, only one of which needs operator action:

1. **Multiple installed, only one running.** A box has both nginx and Apache installed but only nginx listens on port 80. nginx returns `available`, Apache returns `inaccessible` (no listener), the manifest shows both honestly. The dashboard surfaces nginx's metrics; the Apache card is greyed out. **No operator action needed.**

2. **Multiple actively running.** A box has nginx on `:80` and Apache on `:8080`, both happily serving. Both probe `available`, both surface in the manifest, the agent's primary-source resolver picks one as primary for HTTP-request metrics using its built-in preference order (HAProxy > nginx > Apache > Caddy > Traefik). The dashboard renders the primary's data with a "switch source" affordance for the alternatives. **No operator action needed** unless the auto-pick is wrong for this host.

3. **Auto-pick wrong; operator wants to force the choice.** This is what `GEARBOX_AGENT_HTTP_SOURCE` is for. Set it to the gear name (e.g. `nginx`) and the resolver hands the primary slot to that gear instead. Manifest reports `primary_sources.http_requests.reason: "operator override via GEARBOX_AGENT_HTTP_SOURCE"` so dashboards can confirm the override took effect at a glance. If the named gear isn't actually available on this host, the agent logs a warning and falls back to auto-detect — losing HTTP metrics because an override target isn't installed would be worse than serving auto-picked data.

> [!NOTE]
> The override is **per metric category**, not per source. Today only `CategoryHTTPRequests` exists; more land as future metric categories are added (e.g. `container_metrics` when Docker and Podman both gain metrics support). See the agent's `MetricCategory` enum for the current set.

## Out-of-scope limitations

- **Multiple instances of the same source.** Two nginx instances on one box, each with its own config and listen address, can't both be represented in the manifest — the agent treats each source as singular. If you genuinely need this, run two agents (one per instance, each pointed at its own `NGINX_STATUS_URL`). Tracked as a deferred design question on issue #95.
- **TLS verification on non-loopback probes.** The default probe URLs are all loopback (`127.0.0.1`), where self-signed certs are normal. The probe helper disables TLS verification for `127.0.0.1` / `[::1]` / `localhost` URLs only; setting an override URL that points at a public hostname will use full verification, which is correct but means you need a valid cert there.

## Related docs

- [`gear-probes.md`](gear-probes.md) — the probe lifecycle and `ProbeableGear` interface this detection layer builds on.
- Project root [`CLAUDE.md`](../../gearbox/CLAUDE.md) — broader gearbox architecture and the dashboard side.
- Issue [#95](https://github.com/sarg3nt/gearbox/issues/95) — phase-3 design discussion and PR breakdown.
- Issue [#91](https://github.com/sarg3nt/gearbox/issues/91) — parent roadmap for the source-agnostic Metrics gear (Phases 0–8).
