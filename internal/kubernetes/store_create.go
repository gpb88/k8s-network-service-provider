package kubernetes

import (
	"context"
	"fmt"

	v1alpha1 "github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
	"github.com/dcm-project/k8s-network-service-provider/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *K8sNetworkStore) Create(ctx context.Context, spec v1alpha1.NetworkSpec, id string) (*v1alpha1.Network, error) {
	if spec.RoutingLevel != nil && *spec.RoutingLevel == v1alpha1.NetworkSpecRoutingLevelApplication {
		return nil, &store.InvalidArgumentError{
			Field:   "routing_level",
			Message: "application routing not supported",
		}
	}

	if spec.ProviderHints != nil && spec.ProviderHints.Kubernetes != nil {
		nodePorts := spec.ProviderHints.Kubernetes.NodePorts
		if nodePorts != nil && len(*nodePorts) > 0 {
			for _, port := range spec.Ports {
				if port.Name == nil || *port.Name == "" {
					return nil, &store.InvalidArgumentError{
						Field:   "ports",
						Message: "all ports must have names when node_ports are specified",
					}
				}
			}
		}
	}

	labels := dcmLabels(id)
	if spec.Metadata.Labels != nil {
		labels = mergeLabels(labels, *spec.Metadata.Labels)
	}

	service := buildService(spec, s.cfg, labels)

	created, err := s.client.CoreV1().Services(s.cfg.Namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, &store.ConflictError{
				Resource: "network",
				Field:    "metadata.name",
				Value:    spec.Metadata.Name,
			}
		}
		if apierrors.IsInvalid(err) {
			return nil, &store.InvalidArgumentError{
				Field:   "spec",
				Message: fmt.Sprintf("invalid service spec: %v", err),
			}
		}
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	network := serviceToNetwork(created, id)
	return &network, nil
}
