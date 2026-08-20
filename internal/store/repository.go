// Package store defines the network repository interface and error types.
package store

import (
	"context"

	v1alpha1 "github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
)

// NetworkRepository defines the storage interface for network CRUD operations
// and backing infrastructure health checks.
type NetworkRepository interface {
	Create(ctx context.Context, spec v1alpha1.NetworkSpec, id string) (*v1alpha1.Network, error)
	Get(ctx context.Context, networkID string) (*v1alpha1.Network, error)
	List(ctx context.Context, maxPageSize int32, pageToken string) (*v1alpha1.NetworkList, error)
	Delete(ctx context.Context, networkID string) error
	CheckHealth(ctx context.Context) error
}
