FROM golang:1.25.5-alpine AS builder

ARG TARGETARCH=amd64

WORKDIR /src

RUN apk add --no-cache ca-certificates git tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /out/node-agent ./cmd/node-agent

FROM debian:bookworm-slim AS runtime-binaries

ARG TARGETARCH=amd64
ARG XRAY_VERSION=v26.2.6
ARG SINGBOX_VERSION=v1.12.25
ARG MTPROXY_REF=cafc3380a81671579ce366d0594b9a8e450827e9

RUN case "${TARGETARCH}" in \
      amd64) ;; \
      *) \
        echo "unsupported TARGETARCH: ${TARGETARCH}; MTProxy is currently supported only on amd64" >&2; \
        exit 1; \
        ;; \
    esac

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gcc \
    git \
    libc6-dev \
    make \
    libssl-dev \
    tar \
    unzip \
    zlib1g-dev && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    XRAY_ASSET="Xray-linux-64.zip"; \
    SINGBOX_ASSET="sing-box-${SINGBOX_VERSION#v}-linux-amd64.tar.gz"; \
    TMP_DIR="$(mktemp -d)"; \
    mkdir -p /out/bin /out/share/xray; \
    curl -fsSL -o "${TMP_DIR}/xray.zip" "https://github.com/XTLS/Xray-core/releases/download/${XRAY_VERSION}/${XRAY_ASSET}"; \
    unzip -q "${TMP_DIR}/xray.zip" -d "${TMP_DIR}/xray"; \
    install -m 0755 "${TMP_DIR}/xray/xray" /out/bin/xray; \
    if [ -f "${TMP_DIR}/xray/geoip.dat" ]; then install -m 0644 "${TMP_DIR}/xray/geoip.dat" /out/share/xray/geoip.dat; fi; \
    if [ -f "${TMP_DIR}/xray/geosite.dat" ]; then install -m 0644 "${TMP_DIR}/xray/geosite.dat" /out/share/xray/geosite.dat; fi; \
    curl -fsSL -o "${TMP_DIR}/sing-box.tar.gz" "https://github.com/SagerNet/sing-box/releases/download/${SINGBOX_VERSION}/${SINGBOX_ASSET}"; \
    tar -xzf "${TMP_DIR}/sing-box.tar.gz" -C "${TMP_DIR}"; \
    SINGBOX_DIR="$(find "${TMP_DIR}" -maxdepth 1 -type d -name 'sing-box-*' | head -n 1)"; \
    test -n "${SINGBOX_DIR}"; \
    install -m 0755 "${SINGBOX_DIR}/sing-box" /out/bin/sing-box; \
    git clone https://github.com/TelegramMessenger/MTProxy.git "${TMP_DIR}/MTProxy"; \
    cd "${TMP_DIR}/MTProxy"; \
    git checkout "${MTPROXY_REF}"; \
    make; \
    install -m 0755 "${TMP_DIR}/MTProxy/objs/bin/mtproto-proxy" /out/bin/mtproto-proxy; \
    rm -rf "${TMP_DIR}"

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    openssl \
    tzdata \
    zlib1g && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/node-agent /usr/local/bin/node-agent
COPY --from=runtime-binaries /out/bin/xray /usr/local/bin/xray
COPY --from=runtime-binaries /out/bin/sing-box /usr/local/bin/sing-box
COPY --from=runtime-binaries /out/bin/mtproto-proxy /usr/local/bin/mtproto-proxy
COPY --from=runtime-binaries /out/share/xray /usr/local/share/xray

ENV XRAY_LOCATION_ASSET=/usr/local/share/xray
ENV STATE_DIR=/var/lib/zerotracezero-node

VOLUME ["/var/lib/zerotracezero-node"]

ENTRYPOINT ["/usr/local/bin/node-agent"]
