package kubernetes

import (
	"context"

	"github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
)

func (s *K8sNetworkStore) Get(ctx context.Context, networkID string) (*v1alpha1.Network, error) {
	service, err := s.findService(ctx, networkID)
	if err != nil {
		return nil, err
	}

	network := serviceToNetwork(service, networkID)
	return &network, nil
}
