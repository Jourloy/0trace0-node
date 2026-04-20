package nodeagent

import (
	"github.com/jourloy/0trace0-node/internal/controlapi"
)

type applyResult struct {
	Revision        string
	AssignedPorts   map[string]int
	Warnings        []string
	Health          map[string]any
	MTProxyInbounds map[string]controlapi.MTProxyInboundHealth
	Status          string
}

func (a *Agent) applyBundle(bundle *controlapi.ConfigBundle) (applyResult, error) {
	result, err := a.supervisor.ApplyBundle(bundle, a.state.AssignedPorts)
	if err != nil {
		return applyResult{}, err
	}
	return applyResult{
		Revision:        result.Revision,
		AssignedPorts:   result.AssignedPorts,
		Warnings:        result.Warnings,
		Health:          result.Health,
		MTProxyInbounds: result.MTProxyInbounds,
		Status:          runtimeStatus(result.Health),
	}, nil
}
