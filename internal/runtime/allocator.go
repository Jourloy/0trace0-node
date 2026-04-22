package runtime

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

const (
	internalInboundPortMin = 20000
	internalInboundPortMax = 44999
	internalStatsPortMin   = 45000
	internalStatsPortMax   = 49999
)

func AssignPorts(bundle controlapi.ConfigBundle) (map[string]int, error) {
	inbounds := append([]controlapi.ManagedResource{}, bundle.Resources[string(controlapi.KindInbound)]...)
	sort.Slice(inbounds, func(i, j int) bool {
		return inbounds[i].ID < inbounds[j].ID
	})

	assigned := make(map[string]int, len(inbounds))
	used := make(map[int]struct{}, len(inbounds))
	for _, inbound := range inbounds {
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
