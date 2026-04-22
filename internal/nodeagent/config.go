package nodeagent

import (
	"os"
)

type Config struct {
	HTTPAddr      string
	APIToken      string
	NodeName      string
	PublicAddress string
	StateDir      string
}

func LoadConfig() Config {
	return Config{
		HTTPAddr:      getEnv("NODE_HTTP_ADDR", ":8090"),
		APIToken:      getEnv("NODE_API_TOKEN", ""),
		NodeName:      getEnv("NODE_NAME", "node-service"),
		PublicAddress: getEnv("PUBLIC_ADDRESS", ""),
		StateDir:      getEnv("STATE_DIR", "./data/node-agent"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
