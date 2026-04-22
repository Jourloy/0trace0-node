package nodeagent

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

const (
	internalControlAPIAddr = "127.0.0.1:18089"
	udpSessionIdleTTL      = 2 * time.Minute
)

type edgeRoutePlan struct {
	HTTPPort      int
	SOCKS5Port    int
	MTProxyPort   int
	WireGuardPort int
	Hysteria2Port int
	TLSPorts      map[string]int
}

func buildEdgeRoutePlan(bundle *controlapi.ConfigBundle, assignedPorts map[string]int) (edgeRoutePlan, error) {
	plan := edgeRoutePlan{
		TLSPorts: map[string]int{},
	}

	for _, inbound := range bundle.Resources[string(controlapi.KindInbound)] {
		if !inbound.IsEnabled {
			continue
		}
		port := assignedPorts[inbound.ID]
		if port <= 0 {
			return edgeRoutePlan{}, fmt.Errorf("inbound %s has no internal port", inbound.Name)
		}

		switch normalizeInboundProtocol(inbound.Protocol) {
		case "trojan", "vless":
			if inboundTLSMarker(inbound) == "" {
				return edgeRoutePlan{}, fmt.Errorf("%s inbound %s must define serverName or sni for single-port edge ingress", normalizeInboundProtocol(inbound.Protocol), inbound.Name)
			}
			if normalizeInboundProtocol(inbound.Protocol) == "vless" && inboundSecurityMode(inbound) == "none" {
				return edgeRoutePlan{}, fmt.Errorf("vless inbound %s must use tls or reality for single-port edge ingress", inbound.Name)
			}
			marker := strings.ToLower(strings.TrimSpace(inboundTLSMarker(inbound)))
			if existing := plan.TLSPorts[marker]; existing > 0 {
				return edgeRoutePlan{}, fmt.Errorf("duplicate TLS ingress marker %q on single edge port", marker)
			}
			plan.TLSPorts[marker] = port
		case "http":
			if plan.HTTPPort > 0 {
				return edgeRoutePlan{}, fmt.Errorf("only one public http inbound can share a node edge port")
			}
			plan.HTTPPort = port
		case "socks5":
			if isTrue(inbound.Spec["bridge"]) {
				continue
			}
			if plan.SOCKS5Port > 0 {
				return edgeRoutePlan{}, fmt.Errorf("only one public socks5 inbound can share a node edge port")
			}
			plan.SOCKS5Port = port
		case "mtproxy":
			if plan.MTProxyPort > 0 {
				return edgeRoutePlan{}, fmt.Errorf("only one public mtproxy inbound can share a node edge port")
			}
			plan.MTProxyPort = port
		case "wireguard":
			if plan.WireGuardPort > 0 {
				return edgeRoutePlan{}, fmt.Errorf("only one public wireguard inbound can share a node edge port")
			}
			plan.WireGuardPort = port
		case "hysteria2":
			if plan.Hysteria2Port > 0 {
				return edgeRoutePlan{}, fmt.Errorf("only one public hysteria2 inbound can share a node edge port")
			}
			if inboundTLSMarker(inbound) == "" {
				return edgeRoutePlan{}, fmt.Errorf("hysteria2 inbound %s must define serverName or sni for single-port edge ingress", inbound.Name)
			}
			plan.Hysteria2Port = port
		}
	}

	return plan, nil
}

func normalizeInboundProtocol(protocol *string) string {
	if protocol == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*protocol))
}

func inboundSecurityMode(inbound controlapi.ManagedResource) string {
	stream, _ := inbound.Spec["streamSettings"].(map[string]any)
	return strings.ToLower(strings.TrimSpace(stringFromAny(stream["security"])))
}

func inboundTLSMarker(inbound controlapi.ManagedResource) string {
	spec := inbound.Spec
	stream, _ := spec["streamSettings"].(map[string]any)
	tlsSettings, _ := spec["tlsSettings"].(map[string]any)
	streamTLS, _ := stream["tlsSettings"].(map[string]any)
	return firstNonEmpty(
		strings.TrimSpace(stringFromAny(spec["serverName"])),
		strings.TrimSpace(stringFromAny(spec["sni"])),
		strings.TrimSpace(stringFromAny(tlsSettings["serverName"])),
		strings.TrimSpace(stringFromAny(streamTLS["serverName"])),
	)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Service) runEdgeIngress(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	packetConn, err := net.ListenPacket("udp", s.cfg.HTTPAddr)
	if err != nil {
		return err
	}
	defer packetConn.Close()

	s.logger.Info("0trace0-node edge ingress listening", "addr", s.cfg.HTTPAddr)

	errCh := make(chan error, 2)
	go func() {
		errCh <- s.serveTCPIngress(ctx, listener)
	}()
	go func() {
		errCh <- s.serveUDPIngress(ctx, packetConn)
	}()

	select {
	case <-ctx.Done():
		_ = listener.Close()
		_ = packetConn.Close()
		return nil
	case err := <-errCh:
		if err == nil {
			return nil
		}
		_ = listener.Close()
		_ = packetConn.Close()
		return err
	}
}

func (s *Service) serveTCPIngress(ctx context.Context, listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			if isClosedNetworkError(err) {
				return nil
			}
			return err
		}
		go s.handleTCPIngressConn(conn)
	}
}

func (s *Service) handleTCPIngressConn(conn net.Conn) {
	defer conn.Close()

	initial, err := readInitialTCPBytes(conn, 4096, 250*time.Millisecond)
	if err != nil && len(initial) == 0 {
		return
	}

	targetAddr := s.routeTCPIngress(initial)
	if strings.TrimSpace(targetAddr) == "" {
		return
	}

	backend, err := net.DialTimeout("tcp", targetAddr, defaultTimeout)
	if err != nil {
		s.logger.Warn("edge ingress dial failed", "target", targetAddr, "error", err)
		return
	}
	defer backend.Close()

	if len(initial) > 0 {
		if _, err := backend.Write(initial); err != nil {
			return
		}
	}

	copyDone := make(chan struct{}, 2)
	go proxyTCPStream(backend, conn, copyDone)
	go proxyTCPStream(conn, backend, copyDone)
	<-copyDone
}

func (s *Service) routeTCPIngress(initial []byte) string {
	if isReservedHTTPControlRequest(initial) {
		return internalControlAPIAddr
	}

	s.mu.Lock()
	plan := s.edgePlan
	s.mu.Unlock()

	if looksLikeHTTPRequest(initial) && plan.HTTPPort > 0 {
		return tcpBackendAddr(plan.HTTPPort)
	}
	if looksLikeSOCKS5(initial) && plan.SOCKS5Port > 0 {
		return tcpBackendAddr(plan.SOCKS5Port)
	}
	if serverName := tlsServerName(initial); serverName != "" {
		if port := plan.TLSPorts[strings.ToLower(serverName)]; port > 0 {
			return tcpBackendAddr(port)
		}
	}
	if plan.MTProxyPort > 0 {
		return tcpBackendAddr(plan.MTProxyPort)
	}
	return ""
}

func (s *Service) serveUDPIngress(ctx context.Context, packetConn net.PacketConn) error {
	type udpSession struct {
		backend  *net.UDPConn
		target   int
		lastSeen time.Time
	}

	sessions := map[string]*udpSession{}
	var mu sync.Mutex

	closeSession := func(key string, session *udpSession) {
		if session == nil {
			return
		}
		_ = session.backend.Close()
		mu.Lock()
		delete(sessions, key)
		mu.Unlock()
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				mu.Lock()
				for key, session := range sessions {
					_ = session.backend.Close()
					delete(sessions, key)
				}
				mu.Unlock()
				return
			case <-ticker.C:
				now := time.Now()
				mu.Lock()
				for key, session := range sessions {
					if now.Sub(session.lastSeen) < udpSessionIdleTTL {
						continue
					}
					_ = session.backend.Close()
					delete(sessions, key)
				}
				mu.Unlock()
			}
		}
	}()

	buffer := make([]byte, 64*1024)
	for {
		n, clientAddr, err := packetConn.ReadFrom(buffer)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			if isClosedNetworkError(err) {
				return nil
			}
			return err
		}
		payload := append([]byte(nil), buffer[:n]...)
		key := clientAddr.String()

		mu.Lock()
		session := sessions[key]
		mu.Unlock()
		if session == nil {
			targetPort := s.routeUDPIngress(payload)
			if targetPort <= 0 {
				continue
			}
			backendAddr, err := net.ResolveUDPAddr("udp", udpBackendAddr(targetPort))
			if err != nil {
				continue
			}
			backend, err := net.DialUDP("udp", nil, backendAddr)
			if err != nil {
				continue
			}
			session = &udpSession{
				backend:  backend,
				target:   targetPort,
				lastSeen: time.Now(),
			}
			mu.Lock()
			sessions[key] = session
			mu.Unlock()

			go func(sessionKey string, client net.Addr, targetPort int, backendConn *net.UDPConn) {
				reply := make([]byte, 64*1024)
				for {
					n, err := backendConn.Read(reply)
					if err != nil {
						closeSession(sessionKey, session)
						return
					}
					if _, err := packetConn.WriteTo(reply[:n], client); err != nil {
						closeSession(sessionKey, session)
						return
					}
					mu.Lock()
					if current := sessions[sessionKey]; current != nil && current.backend == backendConn {
						current.lastSeen = time.Now()
					}
					mu.Unlock()
				}
			}(key, clientAddr, targetPort, backend)
		}

		mu.Lock()
		if current := sessions[key]; current != nil {
			current.lastSeen = time.Now()
			session = current
		}
		mu.Unlock()
		if session == nil {
			continue
		}
		if _, err := session.backend.Write(payload); err != nil {
			closeSession(key, session)
			continue
		}
	}
}

func (s *Service) routeUDPIngress(payload []byte) int {
	s.mu.Lock()
	plan := s.edgePlan
	s.mu.Unlock()

	switch {
	case plan.Hysteria2Port > 0 && plan.WireGuardPort == 0:
		return plan.Hysteria2Port
	case plan.WireGuardPort > 0 && plan.Hysteria2Port == 0:
		return plan.WireGuardPort
	case plan.WireGuardPort > 0 && looksLikeWireGuard(payload):
		return plan.WireGuardPort
	case plan.Hysteria2Port > 0 && looksLikeQUIC(payload):
		return plan.Hysteria2Port
	case plan.Hysteria2Port > 0:
		return plan.Hysteria2Port
	default:
		return 0
	}
}

func tcpBackendAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

func udpBackendAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

func readInitialTCPBytes(conn net.Conn, maxBytes int, idleTimeout time.Duration) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	buffer := make([]byte, 0, maxBytes)
	chunk := make([]byte, 1024)

	for len(buffer) < maxBytes {
		if err := conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return buffer, err
		}
		n, err := conn.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)
			if len(buffer) >= maxBytes {
				break
			}
			if looksLikeHTTPRequest(buffer) || looksLikeSOCKS5(buffer) || tlsServerName(buffer) != "" {
				break
			}
		}
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			if err == io.EOF {
				break
			}
			_ = conn.SetReadDeadline(time.Time{})
			return buffer, err
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	return buffer, nil
}

func proxyTCPStream(dst net.Conn, src net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	closeWrite(dst)
	done <- struct{}{}
}

func closeWrite(conn net.Conn) {
	type closeWriter interface {
		CloseWrite() error
	}
	if value, ok := conn.(closeWriter); ok {
		_ = value.CloseWrite()
	}
}

func looksLikeHTTPRequest(initial []byte) bool {
	methods := [][]byte{
		[]byte("GET "),
		[]byte("POST "),
		[]byte("PUT "),
		[]byte("HEAD "),
		[]byte("DELETE "),
		[]byte("PATCH "),
		[]byte("OPTIONS "),
		[]byte("CONNECT "),
	}
	for _, method := range methods {
		if bytes.HasPrefix(initial, method) {
			return true
		}
	}
	return false
}

func isReservedHTTPControlRequest(initial []byte) bool {
	if !looksLikeHTTPRequest(initial) {
		return false
	}
	lineEnd := bytes.Index(initial, []byte("\r\n"))
	if lineEnd < 0 {
		lineEnd = bytes.IndexByte(initial, '\n')
	}
	if lineEnd < 0 {
		lineEnd = len(initial)
	}
	parts := strings.Fields(string(initial[:lineEnd]))
	if len(parts) < 2 {
		return false
	}
	target := strings.TrimSpace(parts[1])
	return target == "/health" || strings.HasPrefix(target, "/api/v1/node/")
}

func looksLikeSOCKS5(initial []byte) bool {
	return len(initial) > 0 && initial[0] == 0x05
}

func looksLikeWireGuard(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	switch payload[0] {
	case 1, 2, 3, 4:
		return true
	default:
		return false
	}
}

func looksLikeQUIC(payload []byte) bool {
	return len(payload) > 0 && payload[0]&0x80 != 0
}

func tlsServerName(initial []byte) string {
	if len(initial) == 0 {
		return ""
	}
	var serverName string
	conn := tls.Server(&sniffConn{reader: bytes.NewReader(initial)}, &tls.Config{
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			serverName = strings.ToLower(strings.TrimSpace(info.ServerName))
			return nil, nil
		},
	})
	_ = conn.Handshake()
	return serverName
}

type sniffConn struct {
	reader *bytes.Reader
}

func (c *sniffConn) Read(p []byte) (int, error)       { return c.reader.Read(p) }
func (c *sniffConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *sniffConn) Close() error                     { return nil }
func (c *sniffConn) LocalAddr() net.Addr              { return stubAddr("tcp") }
func (c *sniffConn) RemoteAddr() net.Addr             { return stubAddr("tcp") }
func (c *sniffConn) SetDeadline(time.Time) error      { return nil }
func (c *sniffConn) SetReadDeadline(time.Time) error  { return nil }
func (c *sniffConn) SetWriteDeadline(time.Time) error { return nil }

type stubAddr string

func (a stubAddr) Network() string { return string(a) }
func (a stubAddr) String() string  { return string(a) }

func isClosedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "closed network connection")
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
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

func (s *Service) startInternalControlAPI(ctx context.Context) (*http.Server, error) {
	server := &http.Server{
		Addr:              internalControlAPIAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultTimeout,
	}
	listener, err := net.Listen("tcp", internalControlAPIAddr)
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("internal control API stopped", "error", err)
		}
	}()
	return server, nil
}
