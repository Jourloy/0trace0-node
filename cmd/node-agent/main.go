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

	service, err := nodeagent.New(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize node service", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := service.Run(ctx); err != nil {
		logger.Error("node service stopped with error", "error", err)
		os.Exit(1)
	}
}
