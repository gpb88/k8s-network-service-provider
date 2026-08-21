package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/apiserver"
	"github.com/dcm-project/k8s-network-service-provider/internal/config"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/composite"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/health"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/network"
	k8s "github.com/dcm-project/k8s-network-service-provider/internal/kubernetes"
	"github.com/dcm-project/k8s-network-service-provider/internal/registration"
)

var version = "0.0.1-dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("initializing: %w", err)
	}

	ln, err := net.Listen("tcp", cfg.Server.Address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Server.Address, err)
	}
	defer func() { _ = ln.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	registrar, err := registration.NewRegistrar(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create registrar: %w", err)
	}

	k8sClient, err := k8s.NewClient(cfg.Kubernetes.Kubeconfig)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}
	k8sCfg := k8s.K8sConfig{
		Namespace: cfg.Kubernetes.Namespace,
	}
	store := k8s.NewK8sNetworkStore(k8sClient, k8sCfg, logger)

	healthHandler := health.NewHandler(store, logger, time.Now(), version)
	networkHandler := network.NewHandler(store, logger)
	apiHandler := composite.NewHandler(healthHandler, networkHandler)
	strictAdapter := oapigen.NewStrictHandlerWithOptions(apiHandler, nil, oapigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  apiserver.NewRequestErrorHandler(logger),
		ResponseErrorHandlerFunc: apiserver.NewResponseErrorHandler(logger),
	})

	srv := apiserver.New(cfg, logger, strictAdapter).WithOnReady(func(ctx context.Context) {
		registrar.Start(ctx)
	})

	return srv.Run(ctx, ln)
}
