package nodeagent

import (
	"github.com/jourloy/0trace0-node/internal/controlapi"
	"github.com/jourloy/0trace0-node/internal/runtimeapply"
)

func bundleChecksum(bundle controlapi.ConfigBundle, ports map[string]int) string {
	return runtimeapply.BundleChecksum(bundle, ports)
}
