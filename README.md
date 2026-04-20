# 0trace0 Node Agent

Standalone node runtime for 0trace0.

## Contents

- `cmd/node-agent`: agent entrypoint
- `internal/nodeagent`: sync loop and panel client
- `internal/runtime`: Xray and sing-box renderers
- `internal/runtimeapply`: config validation and process supervision
- `internal/controlapi`: copied wire types shared with the backend API contract
- `configs/examples/node-agent.env.example`: example runtime environment

## Environment

Node agent env uses short backend-style names:

- `API_URL`
- `NODE_TOKEN`
- `NODE_ID`
- `NODE_NAME`
- `PUBLIC_ADDRESS`
- `STATE_DIR`
- `SYNC_INTERVAL`
- `HTTP_TIMEOUT`
- `PORT_MIN`
- `PORT_MAX`
- `MTLS_CERT_FILE`
- `MTLS_KEY_FILE`
- `MTLS_CA_FILE`

See `configs/examples/node-agent.env.example` for a complete example.

### Migration

Legacy `ZEROTRACEZERO_*` and `ZEROTRACEZERO_AGENT_*` env names are no longer supported.

| Old name | New name |
| --- | --- |
| `ZEROTRACEZERO_CONTROL_PLANE_URL` | `API_URL` |
| `ZEROTRACEZERO_AGENT_CONTROL_PLANE_URL` | `API_URL` |
| `ZEROTRACEZERO_NODE_TOKEN` | `NODE_TOKEN` |
| `ZEROTRACEZERO_AGENT_TOKEN` | `NODE_TOKEN` |
| `ZEROTRACEZERO_AGENT_NODE_ID` | `NODE_ID` |
| `ZEROTRACEZERO_NODE_NAME` | `NODE_NAME` |
| `ZEROTRACEZERO_AGENT_NODE_NAME` | `NODE_NAME` |
| `ZEROTRACEZERO_NODE_PUBLIC_ADDRESS` | `PUBLIC_ADDRESS` |
| `ZEROTRACEZERO_AGENT_PUBLIC_ADDRESS` | `PUBLIC_ADDRESS` |
| `ZEROTRACEZERO_AGENT_STATE_DIR` | `STATE_DIR` |
| `ZEROTRACEZERO_AGENT_SYNC_INTERVAL` | `SYNC_INTERVAL` |
| `ZEROTRACEZERO_AGENT_HTTP_TIMEOUT` | `HTTP_TIMEOUT` |
| `ZEROTRACEZERO_AGENT_PORT_MIN` | `PORT_MIN` |
| `ZEROTRACEZERO_AGENT_PORT_MAX` | `PORT_MAX` |
| `ZEROTRACEZERO_AGENT_MTLS_CERT_FILE` | `MTLS_CERT_FILE` |
| `ZEROTRACEZERO_AGENT_MTLS_KEY_FILE` | `MTLS_KEY_FILE` |
| `ZEROTRACEZERO_AGENT_MTLS_CA_FILE` | `MTLS_CA_FILE` |

## Development

```bash
go test ./...
docker build -t 0trace0-node . && docker run --rm \
  -e API_URL=https://api.example.com \
  -e NODE_TOKEN=replace-me \
  -v zerotracezero-node:/var/lib/zerotracezero-node \
  0trace0-node
```

## Docker

The supported runtime flow is Docker-only. `xray` and `sing-box` are installed into the image by the `Dockerfile`, so no runtime env overrides are required.

```bash
docker build -t 0trace0-node . && docker run --rm \
  -e API_URL=https://api.example.com \
  -e NODE_TOKEN=replace-me \
  -v zerotracezero-node:/var/lib/zerotracezero-node \
  0trace0-node
```
