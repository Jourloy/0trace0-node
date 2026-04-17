package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jourloy/0trace0-node/internal/nodeagent"
)

func main() {
	cfg := nodeagent.LoadConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	agent, err := nodeagent.New(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize node agent", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := agent.Run(ctx); err != nil {
		logger.Error("node agent stopped with error", "error", err)
		os.Exit(1)
	}
}
