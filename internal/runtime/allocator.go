package runtime

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

const (
	ControlAPIPublicPort = 8090
	TrojanPublicPort     = 18080
	VLESSPublicPort      = 18081
	MTProxyPublicPort    = 18082
	HTTPPublicPort       = 18083
	SOCKS5PublicPort     = 18084
	Hysteria2PublicPort  = 18085
	WireGuardPublicPort  = 18086

	internalInboundPortMin = 20000
	internalInboundPortMax = 44999
	internalStatsPortMin   = 45000
	internalStatsPortMax   = 49999
)

var fixedPublicProtocolPorts = map[string]int{
	"trojan":    TrojanPublicPort,
	"vless":     VLESSPublicPort,
	"mtproxy":   MTProxyPublicPort,
	"http":      HTTPPublicPort,
	"socks5":    SOCKS5PublicPort,
	"hysteria2": Hysteria2PublicPort,
	"wireguard": WireGuardPublicPort,
}

func FixedPublicProtocolPorts() map[string]int {
	out := make(map[string]int, len(fixedPublicProtocolPorts))
	for protocol, port := range fixedPublicProtocolPorts {
		out[protocol] = port
	}
	return out
}

func AssignPorts(bundle controlapi.ConfigBundle) (map[string]int, error) {
	inbounds := append([]controlapi.ManagedResource{}, bundle.Resources[string(controlapi.KindInbound)]...)
	sort.Slice(inbounds, func(i, j int) bool {
		return inbounds[i].ID < inbounds[j].ID
	})

	assigned := make(map[string]int, len(inbounds))
	used := make(map[int]struct{}, len(inbounds))
	claimedProtocols := map[string]string{}

	for _, inbound := range inbounds {
		port, protocol, ok := fixedPublicPortForInbound(inbound)
		if !ok {
			continue
		}
		if existing := claimedProtocols[protocol]; existing != "" {
			return nil, fmt.Errorf("multiple enabled public %s inbounds cannot share fixed port %d", protocol, port)
		}
		claimedProtocols[protocol] = inbound.ID
		assigned[inbound.ID] = port
		used[port] = struct{}{}
	}

	for _, inbound := range inbounds {
		if _, ok := assigned[inbound.ID]; ok {
			continue
		}
		port, err := assignStablePort("inbound:"+bundle.NodeID+":"+inbound.ID, used, internalInboundPortMin, internalInboundPortMax)
		if err != nil {
			return nil, fmt.Errorf("failed to allocate internal port for %s: %w", inbound.Name, err)
		}
		assigned[inbound.ID] = port
	}
	return assigned, nil
}

func AssignStatsPort(seed string, used map[int]struct{}) (int, error) {
	return assignStablePort("mtproxy-stats:"+seed, used, internalStatsPortMin, internalStatsPortMax)
}

func assignStablePort(seed string, used map[int]struct{}, minPort, maxPort int) (int, error) {
	if minPort <= 0 || maxPort < minPort {
		return 0, fmt.Errorf("invalid internal port range %d-%d", minPort, maxPort)
	}

	rangeSize := maxPort - minPort + 1
	sum := sha256.Sum256([]byte(seed))
	start := int(binary.BigEndian.Uint32(sum[:4]) % uint32(rangeSize))

	for offset := 0; offset < rangeSize; offset++ {
		port := minPort + ((start + offset) % rangeSize)
		if _, ok := used[port]; ok {
			continue
		}
		used[port] = struct{}{}
		return port, nil
	}

	return 0, fmt.Errorf("internal port range %d-%d exhausted", minPort, maxPort)
}

func fixedPublicPortForInbound(inbound controlapi.ManagedResource) (int, string, bool) {
	if !inbound.IsEnabled {
		return 0, "", false
	}
	protocol := normalizeInboundProtocol(inbound.Protocol)
	if protocol == "" {
		return 0, "", false
	}
	if protocol == "socks5" && isInboundBridge(inbound) {
		return 0, "", false
	}
	port, ok := fixedPublicProtocolPorts[protocol]
	return port, protocol, ok
}

func normalizeInboundProtocol(protocol *string) string {
	if protocol == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*protocol))
}

func isInboundBridge(inbound controlapi.ManagedResource) bool {
	switch typed := inbound.Spec["bridge"].(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
