package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func RenderXray(bundle controlapi.ConfigBundle, assignedPorts map[string]int) ([]byte, []string, error) {
	warnings := make([]string, 0)
	inbounds := make([]any, 0)
	outbounds := []any{
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": "block", "protocol": "blackhole"},
	}
	rules := make([]any, 0)

	clients := bundle.Resources[string(controlapi.KindClient)]
	certificates := bundle.Resources[string(controlapi.KindCertificate)]

	for _, inbound := range bundle.Resources[string(controlapi.KindInbound)] {
		protocol := strings.ToLower(valueOrEmpty(inbound.Protocol))
		if protocol == "socks5" && isTrue(inbound.Spec["bridge"]) {
			warnings = append(warnings, fmt.Sprintf("xray skipped bridge inbound %s; rendered in sing-box instead", inbound.Name))
			continue
		}
		if protocol == "hysteria2" || protocol == "wireguard" || protocol == "mtproxy" {
			if protocol == "mtproxy" {
				warnings = append(warnings, fmt.Sprintf("xray skipped inbound %s (%s); rendered in dedicated mtproxy runtime", inbound.Name, protocol))
			}
			continue
		}
		port := assignedPorts[inbound.ID]
		stream := cloneMap(objectFromMap(inbound.Spec, "streamSettings"))
		if status := inboundCertificateStatus(inbound, certificates); status != "" {
			warnings = append(warnings, fmt.Sprintf("xray skipped inbound %s: %s", inbound.Name, status))
			continue
		}
		if tlsMaterial := xrayInboundCertificateMaterial(inbound, certificates); len(tlsMaterial) > 0 {
			tlsSettings := cloneMap(objectFromMap(stream, "tlsSettings"))
			tlsSettings["certificates"] = []any{tlsMaterial}
			stream["tlsSettings"] = tlsSettings
		}
		settings := buildXrayInboundSettings(protocol, inbound, clients)
		entry := map[string]any{
			"tag":      resourceTag(inbound),
			"listen":   stringFromMap(inbound.Spec, "listen", "0.0.0.0"),
			"port":     port,
			"protocol": protocol,
			"settings": settings,
		}
		if len(stream) > 0 {
			entry["streamSettings"] = stream
		}
		if sniffing := objectFromMap(inbound.Spec, "sniffing"); len(sniffing) > 0 {
			entry["sniffing"] = sniffing
		}
		inbounds = append(inbounds, entry)
	}

	for _, outbound := range bundle.Resources[string(controlapi.KindOutbound)] {
		protocol := strings.ToLower(valueOrEmpty(outbound.Protocol))
		tag := resourceTag(outbound)
		switch protocol {
		case "direct":
			outbounds = append(outbounds, map[string]any{"tag": tag, "protocol": "freedom"})
		case "block":
			outbounds = append(outbounds, map[string]any{"tag": tag, "protocol": "blackhole"})
		case "http_proxy":
			outbounds = append(outbounds, map[string]any{
				"tag":      tag,
				"protocol": "http",
				"settings": map[string]any{
					"servers": []any{map[string]any{
						"address": stringFromMap(outbound.Spec, "host", "127.0.0.1"),
						"port":    intFromMap(outbound.Spec, "port", 8080),
						"users": []any{map[string]any{
							"user": stringFromMap(outbound.Spec, "username", ""),
							"pass": stringFromMap(outbound.Spec, "password", ""),
						}},
					}},
				},
			})
		case "socks_proxy":
			outbounds = append(outbounds, map[string]any{
				"tag":      tag,
				"protocol": "socks",
				"settings": map[string]any{
					"servers": []any{map[string]any{
						"address": stringFromMap(outbound.Spec, "host", "127.0.0.1"),
						"port":    intFromMap(outbound.Spec, "port", 1080),
						"users": []any{map[string]any{
							"user": stringFromMap(outbound.Spec, "username", ""),
							"pass": stringFromMap(outbound.Spec, "password", ""),
						}},
					}},
				},
			})
		case "socks_bridge":
			bridgeInboundID := stringFromMap(outbound.Spec, "bridgeInboundId", "")
			bridgePort := assignedPorts[bridgeInboundID]
			if bridgePort <= 0 {
				warnings = append(warnings, fmt.Sprintf("xray skipped bridge outbound %s; missing assigned bridge port", outbound.Name))
				continue
			}
			outbounds = append(outbounds, map[string]any{
				"tag":      tag,
				"protocol": "socks",
				"settings": map[string]any{
					"servers": []any{map[string]any{
						"address": stringFromMap(outbound.Spec, "host", "127.0.0.1"),
						"port":    bridgePort,
					}},
				},
			})
		case "trojan_chain":
			outbounds = append(outbounds, map[string]any{
				"tag":      tag,
				"protocol": "trojan",
				"settings": map[string]any{
					"servers": []any{map[string]any{
						"address":  stringFromMap(outbound.Spec, "host", "127.0.0.1"),
						"port":     intFromMap(outbound.Spec, "port", 443),
						"password": stringFromMap(outbound.Spec, "password", ""),
					}},
				},
				"streamSettings": objectFromMap(outbound.Spec, "streamSettings"),
			})
		case "vless_chain":
			outbounds = append(outbounds, map[string]any{
				"tag":      tag,
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{map[string]any{
						"address": stringFromMap(outbound.Spec, "host", "127.0.0.1"),
						"port":    intFromMap(outbound.Spec, "port", 443),
						"users": []any{map[string]any{
							"id":         stringFromMap(outbound.Spec, "id", ""),
							"encryption": "none",
						}},
					}},
				},
				"streamSettings": objectFromMap(outbound.Spec, "streamSettings"),
			})
		case "selector", "fallback", "wireguard_tunnel":
			warnings = append(warnings, fmt.Sprintf("xray skipped outbound %s (%s); rendered in sing-box instead", outbound.Name, protocol))
		default:
			warnings = append(warnings, fmt.Sprintf("xray skipped unsupported outbound %s (%s)", outbound.Name, protocol))
		}
	}

	for _, policy := range bundle.Resources[string(controlapi.KindRoutingPolicy)] {
		if rule := buildXrayRule(policy); rule != nil {
			rules = append(rules, rule)
		}
	}

	config := map[string]any{
		"log": map[string]any{
			"loglevel": "warning",
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing": map[string]any{
			"domainStrategy": "IPIfNonMatch",
			"rules":          rules,
		},
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	return raw, warnings, err
}

func RenderSingbox(bundle controlapi.ConfigBundle, assignedPorts map[string]int) ([]byte, []string, error) {
	warnings := make([]string, 0)
	inbounds := make([]any, 0)
	outbounds := []any{
		map[string]any{"tag": "direct", "type": "direct"},
		map[string]any{"tag": "block", "type": "block"},
	}
	rules := make([]any, 0)
	clients := bundle.Resources[string(controlapi.KindClient)]
	certificates := bundle.Resources[string(controlapi.KindCertificate)]

	for _, inbound := range bundle.Resources[string(controlapi.KindInbound)] {
		protocol := strings.ToLower(valueOrEmpty(inbound.Protocol))
		port := assignedPorts[inbound.ID]
		switch protocol {
		case "hysteria2":
			if status := inboundCertificateStatus(inbound, certificates); status != "" {
				warnings = append(warnings, fmt.Sprintf("sing-box skipped inbound %s: %s", inbound.Name, status))
				continue
			}
			tls := map[string]any{
				"enabled":     true,
				"server_name": stringFromMap(inbound.Spec, "serverName", stringFromMap(inbound.Spec, "sni", "")),
			}
			if certificate := singboxInboundCertificateMaterial(inbound, certificates); len(certificate) > 0 {
				for key, value := range certificate {
					tls[key] = value
				}
			}
			inbounds = append(inbounds, map[string]any{
				"type":        "hysteria2",
				"tag":         resourceTag(inbound),
				"listen":      stringFromMap(inbound.Spec, "listen", "::"),
				"listen_port": port,
				"users":       buildSingboxUsers(inbound, clients, "password"),
				"tls":         tls,
			})
		case "wireguard":
			inbounds = append(inbounds, map[string]any{
				"type":          "wireguard",
				"tag":           resourceTag(inbound),
				"listen_port":   port,
				"local_address": anySliceFromMap(inbound.Spec, "localAddress", []any{"10.10.0.1/32"}),
				"private_key":   stringFromMap(inbound.Spec, "privateKey", ""),
				"peers":         buildWireguardPeers(inbound, clients),
			})
		case "socks5":
			if !isTrue(inbound.Spec["bridge"]) {
				warnings = append(warnings, fmt.Sprintf("sing-box skipped inbound %s (%s); rendered in xray", inbound.Name, protocol))
				continue
			}
			inbounds = append(inbounds, map[string]any{
				"type":        "socks",
				"tag":         resourceTag(inbound),
				"listen":      stringFromMap(inbound.Spec, "listen", "127.0.0.1"),
				"listen_port": port,
			})
		case "trojan", "vless", "http":
			warnings = append(warnings, fmt.Sprintf("sing-box skipped inbound %s (%s); rendered in xray", inbound.Name, protocol))
		case "mtproxy":
			warnings = append(warnings, fmt.Sprintf("sing-box skipped inbound %s (%s); rendered in dedicated mtproxy runtime", inbound.Name, protocol))
		default:
			warnings = append(warnings, fmt.Sprintf("sing-box skipped unsupported inbound %s (%s)", inbound.Name, protocol))
		}
	}

	for _, outbound := range bundle.Resources[string(controlapi.KindOutbound)] {
		protocol := strings.ToLower(valueOrEmpty(outbound.Protocol))
		tag := resourceTag(outbound)
		switch protocol {
		case "direct":
			outbounds = append(outbounds, map[string]any{"tag": tag, "type": "direct"})
		case "block":
			outbounds = append(outbounds, map[string]any{"tag": tag, "type": "block"})
		case "wireguard_tunnel":
			outbounds = append(outbounds, map[string]any{
				"type":            "wireguard",
				"tag":             tag,
				"server":          stringFromMap(outbound.Spec, "host", ""),
				"server_port":     intFromMap(outbound.Spec, "port", 51820),
				"private_key":     stringFromMap(outbound.Spec, "privateKey", ""),
				"peer_public_key": stringFromMap(outbound.Spec, "peerPublicKey", ""),
				"local_address":   anySliceFromMap(outbound.Spec, "localAddress", []any{"10.11.0.2/32"}),
			})
		case "selector":
			outbounds = append(outbounds, map[string]any{
				"type":      "selector",
				"tag":       tag,
				"outbounds": stringSliceFromMap(outbound.Spec, "outbounds", []string{"direct"}),
				"default":   stringFromMap(outbound.Spec, "default", "direct"),
			})
		case "fallback":
			outbounds = append(outbounds, map[string]any{
				"type":      "urltest",
				"tag":       tag,
				"outbounds": stringSliceFromMap(outbound.Spec, "outbounds", []string{"direct"}),
				"url":       stringFromMap(outbound.Spec, "url", "https://www.gstatic.com/generate_204"),
				"interval":  stringFromMap(outbound.Spec, "interval", "5m"),
			})
		case "http_proxy":
			outbounds = append(outbounds, map[string]any{
				"type":        "http",
				"tag":         tag,
				"server":      stringFromMap(outbound.Spec, "host", ""),
				"server_port": intFromMap(outbound.Spec, "port", 8080),
				"username":    stringFromMap(outbound.Spec, "username", ""),
				"password":    stringFromMap(outbound.Spec, "password", ""),
			})
		case "socks_proxy":
			outbounds = append(outbounds, map[string]any{
				"type":        "socks",
				"tag":         tag,
				"server":      stringFromMap(outbound.Spec, "host", ""),
				"server_port": intFromMap(outbound.Spec, "port", 1080),
				"username":    stringFromMap(outbound.Spec, "username", ""),
				"password":    stringFromMap(outbound.Spec, "password", ""),
			})
		case "trojan_chain":
			outbounds = append(outbounds, map[string]any{
				"type":        "trojan",
				"tag":         tag,
				"server":      stringFromMap(outbound.Spec, "host", ""),
				"server_port": intFromMap(outbound.Spec, "port", 443),
				"password":    stringFromMap(outbound.Spec, "password", ""),
				"tls": map[string]any{
					"enabled":     true,
					"server_name": stringFromMap(outbound.Spec, "serverName", ""),
				},
			})
		case "vless_chain":
			entry := map[string]any{
				"type":        "vless",
				"tag":         tag,
				"server":      stringFromMap(outbound.Spec, "host", ""),
				"server_port": intFromMap(outbound.Spec, "port", 443),
				"uuid":        stringFromMap(outbound.Spec, "id", ""),
			}
			if tls := buildSingboxTLSSettings(outbound); len(tls) > 0 {
				entry["tls"] = tls
			}
			outbounds = append(outbounds, entry)
		case "hysteria2_chain":
			entry := map[string]any{
				"type":        "hysteria2",
				"tag":         tag,
				"server":      stringFromMap(outbound.Spec, "host", ""),
				"server_port": intFromMap(outbound.Spec, "port", 443),
				"password":    stringFromMap(outbound.Spec, "password", ""),
			}
			if serverName := stringFromMap(outbound.Spec, "serverName", ""); serverName != "" {
				entry["tls"] = map[string]any{
					"enabled":     true,
					"server_name": serverName,
				}
			}
			if obfs := stringFromMap(outbound.Spec, "obfs", ""); obfs != "" {
				entry["obfs"] = map[string]any{
					"type":     "salamander",
					"password": obfs,
				}
			}
			outbounds = append(outbounds, entry)
		default:
			warnings = append(warnings, fmt.Sprintf("sing-box skipped unsupported outbound %s (%s)", outbound.Name, protocol))
		}
	}

	for _, policy := range bundle.Resources[string(controlapi.KindRoutingPolicy)] {
		if rule := buildSingboxRule(policy); rule != nil {
			rules = append(rules, rule)
		}
	}

	config := map[string]any{
		"log": map[string]any{
			"level": "warn",
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": rules,
		},
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	return raw, warnings, err
}

func buildXrayInboundSettings(protocol string, inbound controlapi.ManagedResource, clients []controlapi.ManagedResource) map[string]any {
	matched := filterClientsForInbound(clients, inbound.ID)
	switch protocol {
	case "trojan":
		values := make([]any, 0, len(matched))
		for _, client := range matched {
			values = append(values, map[string]any{
				"password": stringFromMap(client.Spec, "password", client.ID),
				"email":    client.Name,
			})
		}
		return map[string]any{"clients": values}
	case "vless":
		values := make([]any, 0, len(matched))
		for _, client := range matched {
			entry := map[string]any{
				"id":         stringFromMap(client.Spec, "id", client.ID),
				"email":      client.Name,
				"encryption": "none",
			}
			if flow := stringFromMap(client.Spec, "flow", ""); flow != "" {
				entry["flow"] = flow
			}
			values = append(values, entry)
		}
		return map[string]any{"clients": values, "decryption": "none"}
	case "http":
		return map[string]any{"accounts": buildHTTPAccounts(matched)}
	case "socks5":
		return map[string]any{"accounts": buildHTTPAccounts(matched), "udp": true}
	default:
		return objectFromMap(inbound.Spec, "settings")
	}
}

func buildHTTPAccounts(clients []controlapi.ManagedResource) []any {
	values := make([]any, 0, len(clients))
	for _, client := range clients {
		values = append(values, map[string]any{
			"user": stringFromMap(client.Spec, "username", client.Name),
			"pass": stringFromMap(client.Spec, "password", client.ID),
		})
	}
	return values
}

func buildSingboxUsers(inbound controlapi.ManagedResource, clients []controlapi.ManagedResource, field string) []any {
	matched := filterClientsForInbound(clients, inbound.ID)
	values := make([]any, 0, len(matched))
	for _, client := range matched {
		values = append(values, map[string]any{
			field: stringFromMap(client.Spec, field, client.ID),
		})
	}
	return values
}

func buildWireguardPeers(inbound controlapi.ManagedResource, clients []controlapi.ManagedResource) []any {
	matched := filterClientsForInbound(clients, inbound.ID)
	values := make([]any, 0, len(matched))
	for _, client := range matched {
		values = append(values, map[string]any{
			"public_key":     stringFromMap(client.Spec, "publicKey", ""),
			"pre_shared_key": stringFromMap(client.Spec, "presharedKey", ""),
			"allowed_ips":    anySliceFromMap(client.Spec, "allowedIps", []any{"0.0.0.0/0"}),
		})
	}
	return values
}

func buildXrayRule(policy controlapi.ManagedResource) map[string]any {
	if !policyEngineAllowed(policy, "xray") {
		return nil
	}
	match := nestedMap(policy.Spec, "match")
	action := nestedMap(policy.Spec, "action")
	rule := map[string]any{
		"type":        "field",
		"outboundTag": stringFromMap(action, "outboundTag", "direct"),
	}
	if tags := stringSliceFromMap(match, "inboundTags", nil); len(tags) > 0 {
		rule["inboundTag"] = tags
	}
	domain := make([]string, 0)
	domain = append(domain, stringSliceFromMap(match, "domains", nil)...)
	for _, suffix := range stringSliceFromMap(match, "domainSuffixes", nil) {
		domain = append(domain, "domain:"+suffix)
	}
	if len(domain) > 0 {
		rule["domain"] = domain
	}
	if values := stringSliceFromMap(match, "ipCidrs", nil); len(values) > 0 {
		rule["ip"] = values
	}
	if values := stringSliceFromMap(match, "protocols", nil); len(values) > 0 {
		rule["protocol"] = values
	}
	if port := stringFromMap(match, "ports", ""); port != "" {
		rule["port"] = port
	}
	if network := stringFromMap(match, "network", ""); network != "" {
		rule["network"] = network
	}
	return rule
}

func buildSingboxRule(policy controlapi.ManagedResource) map[string]any {
	if !policyEngineAllowed(policy, "singbox") {
		return nil
	}
	match := nestedMap(policy.Spec, "match")
	action := nestedMap(policy.Spec, "action")
	rule := map[string]any{
		"outbound": stringFromMap(action, "outboundTag", "direct"),
	}
	if values := stringSliceFromMap(match, "inboundTags", nil); len(values) > 0 {
		rule["inbound"] = values
	}
	if values := stringSliceFromMap(match, "protocols", nil); len(values) > 0 {
		rule["protocol"] = values
	}
	if values := stringSliceFromMap(match, "domains", nil); len(values) > 0 {
		rule["domain"] = values
	}
	if values := stringSliceFromMap(match, "domainSuffixes", nil); len(values) > 0 {
		rule["domain_suffix"] = values
	}
	if values := stringSliceFromMap(match, "ipCidrs", nil); len(values) > 0 {
		rule["ip_cidr"] = values
	}
	if ports := stringFromMap(match, "ports", ""); ports != "" {
		rule["port"] = ports
	}
	if network := stringFromMap(match, "network", ""); network != "" {
		rule["network"] = []string{network}
	}
	return rule
}

func filterClientsForInbound(clients []controlapi.ManagedResource, inboundID string) []controlapi.ManagedResource {
	values := make([]controlapi.ManagedResource, 0)
	for _, client := range clients {
		if stringFromMap(client.Spec, "inboundId", "") == inboundID {
			values = append(values, client)
		}
	}
	return values
}

func resourceTag(resource controlapi.ManagedResource) string {
	if value := stringFromMap(resource.Spec, "tag", ""); value != "" {
		return value
	}
	if strings.TrimSpace(resource.Slug) != "" {
		return resource.Slug
	}
	return resource.ID
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringFromMap(values map[string]any, key, fallback string) string {
	if values == nil {
		return fallback
	}
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func intFromMap(values map[string]any, key string, fallback int) int {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func objectFromMap(values map[string]any, key string) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	if value, ok := values[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func nestedMap(values map[string]any, key string) map[string]any {
	return objectFromMap(values, key)
}

func stringSliceFromMap(values map[string]any, key string, fallback []string) []string {
	if values == nil {
		return fallback
	}
	raw, ok := values[key]
	if !ok {
		return fallback
	}
	switch cast := raw.(type) {
	case []string:
		return cast
	case []any:
		result := make([]string, 0, len(cast))
		for _, item := range cast {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	case string:
		if strings.TrimSpace(cast) == "" {
			return fallback
		}
		return []string{strings.TrimSpace(cast)}
	default:
		return fallback
	}
}

func anySliceFromMap(values map[string]any, key string, fallback []any) []any {
	if values == nil {
		return fallback
	}
	if raw, ok := values[key].([]any); ok && len(raw) > 0 {
		return raw
	}
	return fallback
}

func buildSingboxTLSSettings(outbound controlapi.ManagedResource) map[string]any {
	stream := objectFromMap(outbound.Spec, "streamSettings")
	security := stringFromMap(stream, "security", "")
	serverName := stringFromMap(outbound.Spec, "serverName", "")
	if security == "" && serverName == "" {
		return map[string]any{}
	}
	tls := map[string]any{
		"enabled":     security == "tls" || security == "reality" || serverName != "",
		"server_name": serverName,
	}
	if security == "reality" {
		reality := objectFromMap(outbound.Spec, "realitySettings")
		if fingerprint := stringFromMap(reality, "utlsFingerprint", ""); fingerprint != "" {
			tls["utls"] = map[string]any{
				"enabled":     true,
				"fingerprint": fingerprint,
			}
		}
		realityTLS := map[string]any{
			"enabled":    true,
			"public_key": stringFromMap(reality, "publicKey", ""),
			"short_id":   firstFromSlice(stringSliceFromMap(reality, "shortIds", nil)),
		}
		if shortID := stringFromMap(reality, "shortId", ""); shortID != "" {
			realityTLS["short_id"] = shortID
		}
		tls["reality"] = realityTLS
	}
	return tls
}

func policyEngineAllowed(policy controlapi.ManagedResource, engine string) bool {
	configured := stringFromMap(policy.Spec, "engine", "")
	return configured == "" || configured == engine
}

func isTrue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstFromSlice(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func inboundCertificateStatus(inbound controlapi.ManagedResource, certificates []controlapi.ManagedResource) string {
	protocol := strings.ToLower(valueOrEmpty(inbound.Protocol))
	security := stringFromMap(objectFromMap(inbound.Spec, "streamSettings"), "security", "")
	if security == "reality" || (protocol != "trojan" && protocol != "vless" && protocol != "hysteria2") {
		return ""
	}
	if protocol != "hysteria2" && security != "tls" {
		return ""
	}
	host := firstString(
		stringFromMap(inbound.Spec, "publicAddress", ""),
		stringFromMap(inbound.Spec, "serverName", ""),
		stringFromMap(inbound.Spec, "sni", ""),
	)
	if strings.TrimSpace(host) == "" {
		return "host unresolved"
	}
	for _, certificate := range certificates {
		if !certificate.IsEnabled {
			continue
		}
		if !certificateMatchesHost(certificate, host) {
			continue
		}
		if stringFromMap(certificate.Spec, "certFile", "") != "" && stringFromMap(certificate.Spec, "keyFile", "") != "" {
			return ""
		}
		return "pending certificate"
	}
	return "pending certificate"
}

func xrayInboundCertificateMaterial(inbound controlapi.ManagedResource, certificates []controlapi.ManagedResource) map[string]any {
	security := stringFromMap(objectFromMap(inbound.Spec, "streamSettings"), "security", "")
	if security != "tls" {
		return map[string]any{}
	}
	certificate := matchInboundCertificate(inbound, certificates)
	if certificate == nil {
		return map[string]any{}
	}
	certFile := stringFromMap(certificate.Spec, "certFile", "")
	keyFile := stringFromMap(certificate.Spec, "keyFile", "")
	if certFile == "" || keyFile == "" {
		return map[string]any{}
	}
	return map[string]any{
		"certificateFile": certFile,
		"keyFile":         keyFile,
	}
}

func singboxInboundCertificateMaterial(inbound controlapi.ManagedResource, certificates []controlapi.ManagedResource) map[string]any {
	certificate := matchInboundCertificate(inbound, certificates)
	if certificate == nil {
		return map[string]any{}
	}
	certFile := stringFromMap(certificate.Spec, "certFile", "")
	keyFile := stringFromMap(certificate.Spec, "keyFile", "")
	if certFile == "" || keyFile == "" {
		return map[string]any{}
	}
	return map[string]any{
		"certificate_path": certFile,
		"key_path":         keyFile,
	}
}

func matchInboundCertificate(inbound controlapi.ManagedResource, certificates []controlapi.ManagedResource) *controlapi.ManagedResource {
	host := firstString(
		stringFromMap(inbound.Spec, "publicAddress", ""),
		stringFromMap(inbound.Spec, "serverName", ""),
		stringFromMap(inbound.Spec, "sni", ""),
	)
	if strings.TrimSpace(host) == "" {
		return nil
	}
	nodeID := ""
	if inbound.NodeID != nil {
		nodeID = strings.TrimSpace(*inbound.NodeID)
	}
	for idx := range certificates {
		certificate := &certificates[idx]
		if !certificate.IsEnabled {
			continue
		}
		if certificate.NodeID != nil && strings.TrimSpace(*certificate.NodeID) != "" && strings.TrimSpace(*certificate.NodeID) != nodeID {
			continue
		}
		if certificateMatchesHost(*certificate, host) {
			return certificate
		}
	}
	return nil
}

func certificateMatchesHost(certificate controlapi.ManagedResource, host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, stringFromMap(certificate.Spec, "subject", "")) {
		return true
	}
	for _, value := range stringSliceFromMap(certificate.Spec, "domains", nil) {
		if strings.EqualFold(host, value) {
			return true
		}
	}
	for _, value := range stringSliceFromMap(certificate.Spec, "ipAddresses", nil) {
		if host == value {
			return true
		}
	}
	return false
}
