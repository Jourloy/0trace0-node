package nodeagent

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ControlPlaneURL string
	Token           string
	NodeID          string
	NodeName        string
	PublicAddress   string
	StateDir        string
	SyncInterval    time.Duration
	HTTPTimeout     time.Duration
	PortMin         int
	PortMax         int
	MTLSCertFile    string
	MTLSKeyFile     string
	MTLSCAFile      string
}

func LoadConfig() Config {
	return Config{
		ControlPlaneURL: getEnv("API_URL", "http://localhost:8080"),
		Token:           getEnv("NODE_TOKEN", ""),
		NodeID:          getEnv("NODE_ID", ""),
		NodeName:        getEnv("NODE_NAME", "node-agent"),
		PublicAddress:   getEnv("PUBLIC_ADDRESS", ""),
		StateDir:        getEnv("STATE_DIR", "./data/node-agent"),
		SyncInterval:    parseDuration("SYNC_INTERVAL", 15*time.Second),
		HTTPTimeout:     parseDuration("HTTP_TIMEOUT", 15*time.Second),
		PortMin:         parseInt("PORT_MIN", 20000),
		PortMax:         parseInt("PORT_MAX", 45000),
		MTLSCertFile:    getEnv("MTLS_CERT_FILE", ""),
		MTLSKeyFile:     getEnv("MTLS_KEY_FILE", ""),
		MTLSCAFile:      getEnv("MTLS_CA_FILE", ""),
	}
}

func (c Config) HTTPClient() (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(c.MTLSCertFile) != "" && strings.TrimSpace(c.MTLSKeyFile) != "" {
		cert, err := tls.LoadX509KeyPair(c.MTLSCertFile, c.MTLSKeyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	if strings.TrimSpace(c.MTLSCAFile) != "" {
		caRaw, err := os.ReadFile(c.MTLSCAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caRaw)
		tlsConfig.RootCAs = pool
	}
	return &http.Client{
		Timeout: c.HTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	value := getEnv(key, "")
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseInt(key string, fallback int) int {
	value := getEnv(key, "")
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
