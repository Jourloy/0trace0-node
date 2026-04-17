package runtime

import (
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func AssignPorts(bundle controlapi.ConfigBundle, existing map[string]int, minPort, maxPort int) (map[string]int, error) {
	if minPort <= 0 {
		minPort = 20000
	}
	if maxPort <= minPort {
		maxPort = minPort + 1000
	}

	assigned := make(map[string]int, len(existing))
	used := make(map[int]struct{})
	for id, port := range existing {
		if port > 0 {
			assigned[id] = port
			used[port] = struct{}{}
		}
	}

	for _, inbound := range bundle.Resources[string(controlapi.KindInbound)] {
		if inbound.Port != nil && *inbound.Port > 0 {
			assigned[inbound.ID] = *inbound.Port
			used[*inbound.Port] = struct{}{}
		}
	}

	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	for _, inbound := range bundle.Resources[string(controlapi.KindInbound)] {
		if _, ok := assigned[inbound.ID]; ok {
			continue
		}
		port, err := pickRandomPort(random, used, minPort, maxPort)
		if err != nil {
			return nil, fmt.Errorf("failed to allocate port for %s: %w", inbound.Name, err)
		}
		assigned[inbound.ID] = port
		used[port] = struct{}{}
	}

	return assigned, nil
}

func pickRandomPort(random *rand.Rand, used map[int]struct{}, minPort, maxPort int) (int, error) {
	for attempts := 0; attempts < 2048; attempts++ {
		port := random.Intn(maxPort-minPort) + minPort
		if _, ok := used[port]; ok {
			continue
		}
		if !portAvailable(port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("port range %d-%d exhausted", minPort, maxPort)
}

func portAvailable(port int) bool {
	tcp, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = tcp.Close()

	udp, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = udp.Close()
	return true
}
