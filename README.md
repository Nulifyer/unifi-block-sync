# UniFi Block Sync

UniFi Block Sync periodically queries a Loki metric query for repeatedly blocked client IPs and appends qualifying public IPv4 addresses to a UniFi Network traffic matching list. It uses the local UniFi Network Integration API, so the controller does not need to be exposed to the internet.

The service is intentionally append-only. It preserves existing list entries and never removes an address automatically. Use a dedicated list such as `Auto Blocked IPv4` and reference it from the firewall rule alongside manually managed lists.

## Requirements

- UniFi Network 10.0 or newer with Traffic Matching Lists in the Integration API.
- A local UniFi Network API key with access to the target site.
- Loki reachable from the container.
- A Loki instant query that returns a vector with a `client_ip` label and the block count as its value.

Gateway Fiber is managed through its UniFi Network controller. Set `UNIFI_URL` to the LAN URL of the CloudKey, UniFi OS Server, or Network controller—not the Gateway Fiber data-plane address unless Network runs there.

## UniFi access

1. Open **UniFi Network → Integrations** on the local controller.
2. Create a dedicated API key under a site-restricted service administrator where the installed UniFi version permits it.
3. Give the administrator only the Network configuration access needed to edit Traffic Matching Lists. Do not use an Owner or personal UI account.
4. Create an IPv4 Traffic Matching List named `Auto Blocked IPv4` with one temporary entry. UniFi currently requires a non-empty list when creating it.
5. Configure the firewall rule to drop sources found in that list.
6. Permit the container host or VLAN to reach the controller's HTTPS management port. Do not expose that port to the WAN.

Network 10.x uses these local endpoints:

```text
GET /proxy/network/integration/v1/sites
GET /proxy/network/integration/v1/sites/{siteId}/traffic-matching-lists
PUT /proxy/network/integration/v1/sites/{siteId}/traffic-matching-lists/{listId}
```

The legacy `/proxy/network/api/s/{site}/rest/firewallgroup` API used by Network 9.x is not supported because it is undocumented and uses a different authentication and payload contract.

## Discover the site and list IDs

Run these commands from a machine on the local network. Keep the API key in an environment variable so it does not enter shell history.

```bash
read -rsp "UniFi API key: " UNIFI_API_KEY; export UNIFI_API_KEY
export UNIFI_URL="https://unifi.example.lan"

curl --fail --silent --show-error \
  --header "X-API-Key: ${UNIFI_API_KEY}" \
  "${UNIFI_URL}/proxy/network/integration/v1/sites"

curl --fail --silent --show-error \
  --header "X-API-Key: ${UNIFI_API_KEY}" \
  "${UNIFI_URL}/proxy/network/integration/v1/sites/SITE_UUID/traffic-matching-lists?limit=200"
```

Install the controller's CA certificate on the host or mount it into the container. Use `curl --cacert controller-ca.pem` while testing a private CA. Use `--insecure` and `UNIFI_INSECURE_TLS=true` only for initial diagnosis because they disable server identity verification.

## Loki query

Copy the LogQL query from the Grafana panel, keeping the aggregation grouped by `client_ip`. A compatible query resembles:

```logql
topk(100, sum by (client_ip) (count_over_time({job="goipabuseapi"} | json | event="auth_decision" | decision="blocked" [15m])))
```

Grafana template variables such as `$__range` are unavailable to this service. Replace them with a fixed LogQL duration such as `[15m]`.

## Configuration

Create a `.env` file that is excluded from Git:

```dotenv
LOKI_URL=http://loki:3100
LOKI_QUERY=topk(100, sum by (client_ip) (count_over_time({job="goipabuseapi"} | json | event="auth_decision" | decision="blocked" [15m])))
UNIFI_URL=https://unifi.example.lan
UNIFI_API_KEY=replace-me
UNIFI_SITE_ID=replace-with-site-uuid
UNIFI_LIST_NAME=Auto Blocked IPv4
BLOCK_THRESHOLD=20
POLL_INTERVAL=5m
MAX_ENTRIES=1000
ALLOW_CIDRS=203.0.113.0/24
DRY_RUN=true
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `LOKI_URL` | required | Loki base URL. |
| `LOKI_QUERY` | required | Instant vector query with a `client_ip` label. |
| `LOKI_USERNAME`, `LOKI_PASSWORD` | empty | Optional Loki basic authentication. |
| `UNIFI_URL` | required | Local controller base URL. |
| `UNIFI_API_KEY` | required | Network Integration API key. |
| `UNIFI_SITE_ID` | required | Site UUID returned by the Integration API. |
| `UNIFI_LIST_ID` | empty | Optional list UUID; avoids name lookup. |
| `UNIFI_LIST_NAME` | `Auto Blocked IPv4` | Exact list name used when no ID is set. |
| `BLOCK_THRESHOLD` | `20` | Minimum count returned by Loki. |
| `POLL_INTERVAL` | `5m` | Reconciliation interval. |
| `MAX_ENTRIES` | `1000` | Hard safety limit for all list items. |
| `ALLOW_CIDRS` | empty | Comma-separated addresses that must never be added. |
| `DRY_RUN` | `true` | Logs candidates without writing to UniFi. |
| `UNIFI_CA_FILE` | empty | Mounted PEM CA bundle for the controller certificate. |
| `UNIFI_INSECURE_TLS` | `false` | Disables TLS verification; diagnostic use only. |

Only public IPv4 addresses are accepted. Private, loopback, link-local, carrier-grade NAT, documentation, multicast, and reserved ranges are rejected.

## Run locally

Start in dry-run mode and inspect the logs:

```bash
docker compose --env-file .env up
```

After confirming the selected IPs and the target list, set `DRY_RUN=false`. The service reads the latest list before every update, preserves its current items, deduplicates candidates, and aborts at `MAX_ENTRIES`.

## Build and test

```bash
go test ./...
docker build -t unifi-block-sync:local .
```

Date tags such as `2026.08.11` publish multi-architecture images to `ghcr.io/nulifyer/unifi-block-sync` with the date and `latest` tags.
