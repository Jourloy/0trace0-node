# 0trace0 Node Service

Standalone node runtime for 0trace0.

## Contents

- `cmd/node-agent`: HTTP service entrypoint
- `internal/nodeagent`: node API, local state, and event journal
- `internal/runtime`: Xray and sing-box renderers
- `internal/runtimeapply`: config validation and process supervision
- `internal/controlapi`: copied wire types shared with the backend API contract
- `configs/examples/node-agent.env.example`: example runtime environment

## Environment

Node service env uses local service names:

- `NODE_HTTP_ADDR`
- `NODE_API_TOKEN`
- `NODE_NAME`
- `PUBLIC_ADDRESS`
- `STATE_DIR`

See `configs/examples/node-agent.env.example` for a complete example.

### Migration

Legacy agent env names are no longer used. The node no longer dials the panel directly and instead exposes its own authenticated HTTP API.
Runtime backend ports are now deterministic internal bindings owned by the node and are no longer configured through environment variables.

## Development

```bash
go test ./...
docker build --build-arg TARGETARCH=amd64 -t 0trace0-node . && docker run --rm \
  -e NODE_HTTP_ADDR=:8090 \
  -e NODE_API_TOKEN=replace-me \
  -v zerotracezero-node:/var/lib/zerotracezero-node \
  0trace0-node
```

## Docker

The supported runtime flow is Docker-only. `xray`, `sing-box`, and `mtproto-proxy` are installed into the image by the `Dockerfile`, so no runtime env overrides are required.

The Docker image is currently supported only on `linux/amd64`. Builds for `arm64` and other architectures fail intentionally until `MTProxy` has a supported non-`amd64` path.

```bash
docker build --build-arg TARGETARCH=amd64 -t 0trace0-node . && docker run --rm \
  -e NODE_HTTP_ADDR=:8090 \
  -e NODE_API_TOKEN=replace-me \
  -p 8090:8090 \
  -v zerotracezero-node:/var/lib/zerotracezero-node \
  0trace0-node
```
