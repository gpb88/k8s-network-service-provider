// Package kubernetes implements Kubernetes-backed operations for the network SP.
package kubernetes

import (
	"context"
	"log/slog"

	"github.com/dcm-project/k8s-network-service-provider/internal/store"
	"k8s.io/client-go/kubernetes"
)

// K8sNetworkStore implements store.HealthChecker using the Kubernetes API.
type K8sNetworkStore struct {
	client kubernetes.Interface
	cfg    K8sConfig
	logger *slog.Logger
}

// NewK8sNetworkStore creates a new K8sNetworkStore with the given client, config, and logger.
func NewK8sNetworkStore(client kubernetes.Interface, cfg K8sConfig, logger *slog.Logger) *K8sNetworkStore {
	return &K8sNetworkStore{
		client: client,
		cfg:    cfg,
		logger: logger,
	}
}

var _ store.HealthChecker = (*K8sNetworkStore)(nil)

// CheckHealth verifies the backing Kubernetes cluster is reachable.
func (s *K8sNetworkStore) CheckHealth(_ context.Context) error {
	_, err := s.client.Discovery().ServerVersion()
	if err != nil {
		s.logger.Warn("kubernetes health check failed", "error", err)
		return err
	}
	return nil
}
