package publicaddr

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

var probeEndpoints = []string{
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://ifconfig.me/ip",
}

func DetectExternalIPv4(ctx context.Context) string {
	client := &http.Client{Timeout: 3 * time.Second}
	for _, endpoint := range probeEndpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		res, err := client.Do(req)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 256))
		_ = res.Body.Close()
		if readErr != nil || res.StatusCode >= 400 {
			continue
		}
		value := strings.TrimSpace(string(body))
		if IsPublicIPv4(value) {
			return value
		}
	}
	return ""
}

func EffectiveAddress(ctx context.Context, configured string) string {
	if IsUsableHost(configured) {
		return strings.TrimSpace(configured)
	}
	if detected := DetectExternalIPv4(ctx); detected != "" {
		return detected
	}
	return strings.TrimSpace(configured)
}

func IsUsableHost(value string) bool {
	trimmed := normalizeHost(value)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "localhost") || strings.HasSuffix(strings.ToLower(trimmed), ".local") {
		return false
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		return isPublicIP(ip)
	}
	if strings.ContainsAny(trimmed, " /\\") {
		return false
	}
	return strings.Contains(trimmed, ".")
}

func IsWildcardOrLocal(value string) bool {
	trimmed := normalizeHost(value)
	switch trimmed {
	case "", "0.0.0.0", "::":
		return true
	}
	ip := net.ParseIP(trimmed)
	return ip != nil && !isPublicIP(ip)
}

func IsPublicIPv4(value string) bool {
	ip := net.ParseIP(normalizeHost(value))
	if ip == nil {
		return false
	}
	ip = ip.To4()
	return ip != nil && isPublicIP(ip)
}

func normalizeHost(value string) string {
	return strings.Trim(strings.TrimSpace(value), "[]")
}

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	return ip.IsGlobalUnicast()
}
