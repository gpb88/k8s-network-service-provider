# Kubernetes Network Service Provider

A [DCM](https://github.com/dcm-project) service provider for managing network
services on Kubernetes clusters using `Service` resources.

## Overview

This service provider maps the portable `network` service type to Kubernetes
Services (ClusterIP, NodePort, LoadBalancer). It exposes an AEP-compliant REST
API, registers with the DCM control plane, and reports service lifecycle status
via CloudEvents.

See the [k8s-network-sp enhancement](https://github.com/dcm-project/enhancements/blob/main/enhancements/k8s-network-sp/k8s-network-sp.md)
for the full design.

## Features

- **Network service lifecycle** — create, read, and delete network services via
  REST API (v1; no UPDATE/day-2 operations)
- **Kubernetes-native** — each network maps to a Kubernetes `Service`
- **Service type inference** — ClusterIP, NodePort, or LoadBalancer inferred from
  `routing_level` + `node_ports` presence
- **Portable contract** — implements the DCM `network` service type with
  `provider_hints.kubernetes` for selector, cluster_ip, and node_ports
- **Status monitoring** — watches Services and publishes status changes
  (PENDING, READY, DELETED) via CloudEvents on NATS subject `dcm.network`
- **Auto-registration** — registers with the DCM Service Provider Manager on
  startup, with exponential backoff retry
- **Health check** — exposes a resource-relative health endpoint for DCM polling
- **AEP-compliant API** — OpenAPI v1alpha1 contract with request validation
- **RFC 7807 errors** — problem details for all error responses

## Development

### Prerequisites

- Go 1.26.0+
- `make`
- `golangci-lint` (for `make lint`)

### Build

```bash
make build
```

### Test

```bash
make test
```

### Lint

```bash
make lint       # Run golangci-lint
make check      # fmt + vet + lint + test (full validation)
```

### Code Generation

```bash
make generate-api         # Regenerate types, server, and client from OpenAPI
make check-generate-api   # Verify generated code is up to date (CI)
make check-aep            # Validate OpenAPI against AEP (requires spectral)
```

Generated files (do not edit manually):

- `api/v1alpha1/types.gen.go`
- `api/v1alpha1/spec.gen.go`
- `internal/api/server/server.gen.go`
- `pkg/client/client.gen.go`

## API

Contract: `api/v1alpha1/openapi.yaml`

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1alpha1/networks/health` | Health check |
| POST | `/api/v1alpha1/networks` | Create network |
| GET | `/api/v1alpha1/networks` | List networks |
| GET | `/api/v1alpha1/networks/{network_id}` | Get network |
| DELETE | `/api/v1alpha1/networks/{network_id}` | Delete network |

## Project Structure

```
.
├── api/v1alpha1/              # OpenAPI spec and generated types
├── cmd/k8s-network-service-provider/
├── internal/
│   ├── api/server/            # Generated strict server interface
│   ├── apiserver/
│   ├── config/
│   ├── handlers/
│   ├── kubernetes/
│   └── registration/
├── pkg/client/                # Generated HTTP client
├── .ai/
│   ├── specs/
│   └── test-plans/
└── Makefile
```

## License

Apache License 2.0 — see [LICENSE](LICENSE).
