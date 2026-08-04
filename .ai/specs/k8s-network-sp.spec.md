# Specification: K8s Network Service Provider

## 1. Overview

The Kubernetes Network Service Provider (K8s Network SP) is a REST API that
manages network services on Kubernetes clusters using `Service` resources. It
exposes endpoints for creating, reading, and deleting network services;
integrates with the DCM Service Provider Registry; reports resource status via
CloudEvents over NATS; and exposes a health endpoint for DCM control plane
polling.

This is a **DCM adapter**, not a networking backend. The SP translates portable
`network` service type requests into Kubernetes Services. The underlying
networking implementation (kube-proxy, cloud load balancers, MetalLB) remains
the responsibility of the cluster infrastructure.

**Version scope (v1):**

- Network service lifecycle: CREATE, READ, DELETE -- as defined in
  `api/v1alpha1/openapi.yaml` (no UPDATE; day-2 operations are out of scope)
- Kubernetes Services only (ClusterIP, NodePort, LoadBalancer)
- Service type inferred from `routing_level` + `node_ports` presence -- not a
  provider-level config default
- No Ingress, NetworkPolicy, or standalone Endpoints resources
- Single configured namespace for all managed Services (not overridable per
  service)
- `provider_hints.kubernetes` fields: `selector`, `cluster_ip`, `node_ports`

**Reference documents:**

- [K8s Network SP Enhancement](https://github.com/dcm-project/enhancements/blob/main/enhancements/k8s-network-sp/k8s-network-sp.md)
- [SP Registration Flow](https://github.com/dcm-project/enhancements/blob/main/enhancements/sp-registration-flow/sp-registration-flow.md)
- [SP Health Check](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-provider-health-check/service-provider-health-check.md)
- [SP Status Reporting](https://github.com/dcm-project/enhancements/blob/main/enhancements/state-management/service-provider-status-reporting.md)
- [Service Type Definitions](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-type-definitions/service-type-definitions.md)
- OpenAPI Spec: `api/v1alpha1/openapi.yaml` (source of truth for API contract)

---

## 2. Architecture

```
                                     +------------------+
                                     |   DCM Control    |
                                     |     Plane        |
                                     +--------+---------+
                                              |
                          +-------------------+-------------------+
                          ^                   |                   |
                          |                   |                   |
                   Registration         Health Poll         NATS Messages
                   POST /providers      GET .../health      (CloudEvents)
                          |                   |                   |
                          |                   v                   |
+-------------------------+-------------------+-------------------+--------+
|                    K8s Network Service Provider                          |
|                                                                          |
|  +-------------+  +----------------+  +------------------+               |
|  | HTTP Server |--| Network        |--| Network Store    |               |
|  | (chi)       |  | Handlers       |  | (interface)      |               |
|  +------+------+  | + Health       |  +--------+---------+               |
|         |         +----------------+           |                         |
|  +------+------+                     +---------+---------+               |
|  | DCM Reg.    |                     | K8s Store (impl)  |               |
|  | Client      |                     +---------+---------+               |
|  +-------------+                               |                         |
|                                      +---------+---------+               |
|                                      | Status Monitor    |------> NATS   |
|                                      | (Svc Informer)    |  dcm.network  |
|                                      +-------------------+               |
+-------------------------------------------------------------------------+
                                                |
                                      +---------+---------+
                                      |  Kubernetes API   |
                                      |  (Services)       |
                                      +-------------------+
```

Each SP instance connects to exactly one Kubernetes cluster API and one
configured namespace. Multiple SP instances may register against the same
cluster when isolation requires separate namespaces or registrations.

---

## 3. Topic Dependency Graph

| # | Topic                                    | Prefix   | Depends On |
|---|------------------------------------------|----------|------------|
| 1 | HTTP Server                              | HTTP     | -          |
| 2 | Health Service                           | HLT      | 1, 4       |
| 3 | Network API Handlers                     | API      | 1, 4       |
| 4 | Kubernetes Integration & Store           | K8S, STR | -          |
| 5 | Resource Status Monitoring & Reporting   | MON      | 4          |
| 6 | DCM Registration                         | REG      | 1          |

```
Topic 1: HTTP Server              (independent)
Topic 4: K8s Integration & Store  (independent)
  |         |
  |         +---> Topic 5: Status Monitoring    (depends on 4)
  |
  +---> Topic 2: Health Service         (depends on 1, 4)
  +---> Topic 3: Network API Handlers   (depends on 1, 4)
  +---> Topic 6: DCM Registration       (depends on 1)
```

Topics 1 and 4 can be delivered in parallel. Topics 2, 3, 5, and 6 depend on
their respective prerequisites.

> **Note:** Handler tests mock the network store interface; K8s store tests use
> `client-go/kubernetes/fake` where applicable.

---

## 4. Topic Specifications

### 4.1 HTTP Server

#### Overview

Foundation layer: chi-based HTTP server with graceful shutdown, signal handling,
configuration loading from environment variables, and route registration for all
OpenAPI-defined endpoints. Network endpoints are under `/api/v1alpha1`, and the
health endpoint is at `/api/v1alpha1/networks/health`.

Out of scope: TLS termination (handled by infrastructure/ingress),
authentication/authorization middleware, rate limiting.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-HTTP-010 | The SP MUST start an HTTP server on the configured address | MUST | |
| REQ-HTTP-020 | The SP MUST register all OpenAPI-defined routes. Network endpoints under `/api/v1alpha1`, health at `/api/v1alpha1/networks/health` | MUST | DD-010 |
| REQ-HTTP-030 | The SP MUST initiate graceful shutdown on SIGTERM: stop new connections, drain in-flight requests within configured timeout, exit cleanly | MUST | |
| REQ-HTTP-040 | The SP MUST initiate graceful shutdown on SIGINT, behaving identically to REQ-HTTP-030 | MUST | |
| REQ-HTTP-050 | The SP MUST load configuration values from environment variables | MUST | |
| REQ-HTTP-060 | The SP MUST log each HTTP request at INFO level including method, path, response status code, and duration | MUST | |
| REQ-HTTP-070 | The SP MUST catch panics in HTTP handlers and return an RFC 7807 INTERNAL error response. Panics that signal intentional connection abort MUST be re-raised. If the response has already started streaming, the panic MUST be logged without writing a response body. Recovery middleware MUST be applied as the outermost middleware layer to ensure panics in any middleware are caught | MUST | |
| REQ-HTTP-080 | The SP MUST log server lifecycle events including listen address on startup | MUST | |
| REQ-HTTP-090 | The SP MUST return 400 Bad Request with RFC 7807 error body for malformed requests | MUST | |
| REQ-HTTP-091 | The API framework layer MUST return RFC 7807 error responses for request parsing and response serialization failures, not plain text | MUST | |
| REQ-HTTP-110 | The SP SHOULD enforce a configurable per-request timeout, cancelling the request context after the deadline | SHOULD | |

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| server.address | SP_SERVER_ADDRESS | :8080 | Listen address (host:port) |
| server.shutdownTimeout | SP_SERVER_SHUTDOWN_TIMEOUT | 15s | Graceful shutdown drain timeout |
| server.readTimeout | SP_SERVER_READ_TIMEOUT | 15s | HTTP read timeout |
| server.writeTimeout | SP_SERVER_WRITE_TIMEOUT | 15s | HTTP write timeout |
| server.idleTimeout | SP_SERVER_IDLE_TIMEOUT | 60s | HTTP idle timeout |
| server.requestTimeout | SP_SERVER_REQUEST_TIMEOUT | 30s | Per-request context timeout |

#### Acceptance Criteria

##### AC-HTTP-010: Server starts on configured address

- Validates: REQ-HTTP-010
- **Given** valid configuration is provided
- **When** the SP starts
- **Then** the HTTP server MUST begin listening on the configured address

##### AC-HTTP-020: Route registration

- Validates: REQ-HTTP-020
- **Given** the HTTP server has started
- **When** a request is made to any defined endpoint (e.g., `/api/v1alpha1/networks/health`, `/api/v1alpha1/networks`)
- **Then** the request MUST be routed to the corresponding handler

##### AC-HTTP-030: Graceful shutdown on SIGTERM

- Validates: REQ-HTTP-030
- **Given** the HTTP server is running
- **When** SIGTERM is received
- **Then** the server MUST stop accepting new connections
- And the server MUST drain in-flight requests within the configured shutdown timeout
- And the server MUST exit cleanly after draining or timeout

##### AC-HTTP-040: Graceful shutdown on SIGINT

- Validates: REQ-HTTP-040
- **Given** the HTTP server is running
- **When** SIGINT is received
- **Then** the server MUST behave identically to REQ-HTTP-030

##### AC-HTTP-050: Configuration from environment variables

- Validates: REQ-HTTP-050
- **Given** environment variables are set (e.g., SP_SERVER_ADDRESS=:9090)
- **When** the SP starts
- **Then** the SP MUST use the values from the environment variables

##### AC-HTTP-080: Lifecycle logging

- Validates: REQ-HTTP-080
- **Given** the SP starts or stops
- **When** the server begins listening or initiates shutdown
- **Then** the SP MUST log the event including the listen address on startup

##### AC-HTTP-060: Request logging

- Validates: REQ-HTTP-060
- **Given** any HTTP request is processed
- **When** the response is sent
- **Then** the SP MUST log at INFO level with method, path, status code, and duration

##### AC-HTTP-070: Panic recovery

- Validates: REQ-HTTP-070
- **Given** a handler panics during request processing
- **When** the panic is caught
- **Then** the response MUST be HTTP 500 with RFC 7807 body (type=INTERNAL)
- And the panic and stack trace MUST be logged at ERROR level
- And panics that signal intentional connection abort MUST be re-raised
- And if the response has already started streaming, a warning MUST be logged without writing a response body

##### AC-HTTP-090: Malformed request handling

- Validates: REQ-HTTP-090
- **Given** a request with invalid parameters (e.g., malformed query params)
- **When** the request reaches the router
- **Then** the SP MUST return a 400 Bad Request with an RFC 7807 error body

##### AC-HTTP-091: Framework-layer error responses

- Validates: REQ-HTTP-091
- **Given** the API framework layer encounters a request parsing or response serialization failure
- **When** an error response is generated
- **Then** the error response MUST be RFC 7807 with `Content-Type: application/problem+json`
- And INTERNAL errors MUST NOT expose implementation details

##### AC-HTTP-110: Request timeout

- Validates: REQ-HTTP-110
- **Given** a configurable request timeout is set (default 30s)
- **When** a request exceeds the timeout
- **Then** the request context MUST be cancelled

#### Dependencies

None -- independently deliverable.

---

### 4.2 Health Service

#### Overview

Implementation of `GET /api/v1alpha1/networks/health` as defined in the OpenAPI
spec. This endpoint is polled by the DCM control plane every 10 seconds to
determine SP liveness and backing provider health. The endpoint checks
Kubernetes API server reachability and reports `status: "healthy"` or
`status: "unhealthy"` per the DCM health model.

Out of scope: NATS connectivity checks, readiness vs liveness distinction
(future enhancement).

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-HLT-010 | The SP MUST expose `GET /api/v1alpha1/networks/health` and return HTTP 200 OK. The SPRM constructs health URLs as `{registered_endpoint}/health`, so the health endpoint MUST be at the resource-relative path | MUST | DD-010 |
| REQ-HLT-020 | The health response MUST return a JSON body conforming to the `Health` schema with `status`, `type`, `path`, `version`, and `uptime` fields. The `status` field MUST be `"healthy"` when the backing K8s cluster is reachable, or `"unhealthy"` when it is not | MUST | DD-060 |
| REQ-HLT-030 | The response MUST set `Content-Type: application/json` | MUST | |
| REQ-HLT-040 | The health endpoint MUST be lightweight and return quickly, suitable for 10-second polling intervals. The only external call permitted is a Kubernetes API server version discovery request | MUST | |
| REQ-HLT-050 | The health endpoint MUST check backing K8s cluster liveness by calling the Kubernetes API server's version discovery endpoint | MUST | DD-060 |
| REQ-HLT-060 | When the K8s cluster is unreachable or the discovery call fails, the health endpoint MUST return HTTP 200 with `status: "unhealthy"`. All other response fields (`type`, `path`, `version`, `uptime`) MUST still be populated | MUST | DD-060 |
| REQ-HLT-070 | The `CheckHealth` method MUST be part of the store interface so that the store implementation is the single source of backing-infrastructure interaction | MUST | DD-030; REQ-STR-010 |

#### Acceptance Criteria

##### AC-HLT-010: Health endpoint availability

- Validates: REQ-HLT-010
- **Given** the HTTP server is running
- **When** a GET request is made to `/api/v1alpha1/networks/health`
- **Then** the SP MUST return HTTP 200 OK

##### AC-HLT-020: Health response body -- healthy

- Validates: REQ-HLT-020, REQ-HLT-050
- **Given** the SP is running and the backing K8s cluster is reachable
- **When** `GET /api/v1alpha1/networks/health` is called
- **Then** the response body MUST contain:
  - `status`: `"healthy"`
  - `type`: `"k8s-network-service-provider.dcm.io/health"`
  - `path`: `"health"`
  - `version`: SP build version (string)
  - `uptime`: seconds since SP started (integer)

##### AC-HLT-025: Health response body -- unhealthy

- Validates: REQ-HLT-020, REQ-HLT-060
- **Given** the SP is running but the backing K8s cluster is unreachable
- **When** `GET /api/v1alpha1/networks/health` is called
- **Then** the response MUST be HTTP 200 OK
- And the response body MUST contain:
  - `status`: `"unhealthy"`
  - `type`: `"k8s-network-service-provider.dcm.io/health"`
  - `path`: `"health"`
  - `version`: SP build version (string)
  - `uptime`: seconds since SP started (integer)

##### AC-HLT-030: Health response content type

- Validates: REQ-HLT-030
- **Given** any call to the health endpoint
- **When** the response is returned
- **Then** the `Content-Type` header MUST be `application/json`

##### AC-HLT-040: Lightweight execution

- Validates: REQ-HLT-040
- **Given** the DCM control plane polls the health endpoint
- **When** the request is processed
- **Then** the handler MUST only perform a Kubernetes API server version discovery call (no Service listing, no NATS checks, no DB queries)

#### Dependencies

- Topic 1 (HTTP Server) -- health route registration
- Topic 4 (Kubernetes Integration & Store) -- `CheckHealth` implementation

---

### 4.3 Network API Handlers

#### Overview

Implement all network operations defined in the OpenAPI specification. Wire each
endpoint to the `NetworkRepository` interface. Map store and validation errors
to RFC 7807 responses.

The portable contract is `NetworkSpec` in the create request `Network.spec`;
responses use the full `Network` resource with read-only `id`, `path`, `status`,
and `kubernetes` runtime state. The Kubernetes Service type is inferred from
`routing_level` + `node_ports` presence per the Service Type Inference table.

Out of scope: authentication/authorization (401/403), UPDATE / day-2 operations,
Ingress resources (`routing_level: application`).

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-API-010 | The SP MUST implement all API operations defined in the OpenAPI specification | MUST | |
| REQ-API-020 | POST `/api/v1alpha1/networks` MUST accept a `Network` body (portable fields in `spec`) and return 201 Created with a `Network` | MUST | AEP-133 |
| REQ-API-030 | When no `id` query parameter is provided, the server MUST generate a DCM instance ID conforming to AEP-122 | MUST | |
| REQ-API-040 | When an `id` query parameter is provided, the server MUST use it as the DCM instance ID | MUST | |
| REQ-API-041 | When an `id` query parameter is provided, it MUST conform to AEP-122 pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` | MUST | |
| REQ-API-050 | `spec.service_type` in the request MUST be `"network"` | MUST | |
| REQ-API-060 | POST `spec` MUST require `ports` (non-empty array), `metadata.name`, and `service_type` | MUST | |
| REQ-API-070 | `metadata.name` MUST conform to Kubernetes DNS-1123 subdomain label rules and MUST NOT exceed 63 characters | MUST | OpenAPI `maxLength: 63` |
| REQ-API-080 | Newly created networks MUST have `status` set to `PENDING` when the Service type is LoadBalancer (waiting for external IP), or `READY` for ClusterIP and NodePort services | MUST | |
| REQ-API-090 | The create response MUST populate read-only fields: `id`, `path`, `status`, `metadata.namespace`, and `kubernetes` (type, cluster_ip, selector) | MUST | |
| REQ-API-100 | POST MUST return 409 Conflict when a Service with the same `metadata.name` already exists in the configured namespace | MUST | |
| REQ-API-105 | POST MUST return 409 Conflict when a NodePort value conflicts with an existing Service | MUST | |
| REQ-API-110 | POST MUST return 400 Bad Request when `routing_level` is `application` (not supported in v1) | MUST | |
| REQ-API-120 | GET `/api/v1alpha1/networks` MUST return a paginated `NetworkList` | MUST | |
| REQ-API-121 | GET MUST return 200 OK with an empty `results` array when no networks exist | MUST | |
| REQ-API-130 | GET MUST support `max_page_size` (default 50, max 1000) and `page_token` | MUST | |
| REQ-API-140 | GET `/api/v1alpha1/networks/{network_id}` MUST return 200 with the network when found | MUST | |
| REQ-API-150 | GET MUST return 404 when no Service matches the `network_id` (`dcm-instance-id` label) | MUST | |
| REQ-API-210 | DELETE `/api/v1alpha1/networks/{network_id}` MUST return 204 when delete is accepted | MUST | |
| REQ-API-211 | A GET request for a deleted network MUST return 404 Not Found | MUST | |
| REQ-API-220 | DELETE MUST return 404 when the network does not exist | MUST | |
| REQ-API-230 | All error responses MUST use `Content-Type: application/problem+json` and RFC 7807 `Error` schema with at minimum `type` and `title` fields | MUST | |
| REQ-API-231 | Error types MUST map to appropriate HTTP status codes per the error mapping table | MUST | |
| REQ-API-240 | `provider_hints` MUST be accepted on input; `provider_hints.kubernetes.selector`, `cluster_ip`, and `node_ports` are acted on in v1 | MUST | DD-020 |

**Service Type Inference (REQ-API-080):**

| routing_level | node_ports present? | K8s Service Type | Behavior |
|:---|:---|:---|:---|
| omitted | No | ClusterIP | Basic internal service |
| omitted | Yes | NodePort | Exposed on all node IPs with specified ports |
| `network` | No | LoadBalancer | LoadBalancer, node_ports auto-allocated by K8s |
| `network` | Yes | LoadBalancer | LoadBalancer, uses specified node_ports |
| `application` | No | Error | Not supported in v1 (return error) |
| `application` | Yes | Error | Invalid: node_ports not applicable for Ingress |

**Error type mapping (REQ-API-231):**

| Error Condition | HTTP Status | Error Type |
|-----------------|-------------|------------|
| Invalid request body / validation | 400 | INVALID_ARGUMENT |
| Unsupported routing_level: application | 400 | INVALID_ARGUMENT |
| Network not found | 404 | NOT_FOUND |
| Service name already exists / NodePort conflict | 409 | ALREADY_EXISTS |
| Unexpected error | 500 | INTERNAL |

#### Acceptance Criteria

##### AC-API-010: Create network -- success

- Validates: REQ-API-020, REQ-API-080, REQ-API-090
- **Given** a valid create `Network` with `spec.ports`, `spec.metadata.name`, and `spec.service_type: network`
- **When** POST `/api/v1alpha1/networks` is called
- **Then** the response MUST be 201 Created with a `Network` including appropriate `status` (`READY` for ClusterIP/NodePort, `PENDING` for LoadBalancer)

##### AC-API-011: Create network -- server-generated ID

- Validates: REQ-API-030
- **Given** POST `/api/v1alpha1/networks` is called without `?id=`
- **When** the network is created
- **Then** the response MUST contain a server-generated ID as the `id` field

##### AC-API-012: Create network -- client-specified ID

- Validates: REQ-API-040, REQ-API-041
- **Given** POST `/api/v1alpha1/networks?id=web-frontend-svc` is called
- **When** the network is created
- **Then** the response `id` field MUST be `"web-frontend-svc"`

##### AC-API-013: Create network -- read-only fields

- Validates: REQ-API-090
- **Given** a network is created successfully
- **When** the response is returned
- **Then** the following fields MUST be populated:
  - `id`: server-generated or client-specified DCM instance ID (AEP-122)
  - `path`: `"networks/{network_id}"`
  - `status`: `"PENDING"` (LoadBalancer) or `"READY"` (ClusterIP/NodePort)
  - `metadata.namespace`: configured namespace
  - `kubernetes.type`: inferred K8s Service type
  - `kubernetes.cluster_ip`: auto-assigned or user-specified cluster IP

##### AC-API-020: Create network -- conflict on duplicate name

- Validates: REQ-API-100
- **Given** a Service named `web-frontend` already exists in the configured namespace
- **When** POST is called with `metadata.name: web-frontend`
- **Then** the response MUST be 409 Conflict with an RFC 7807 error body
- And the existing Service MUST NOT be modified

##### AC-API-021: Create network -- validation failure

- Validates: REQ-API-060, REQ-API-070
- **Given** a request body missing required fields (e.g., no `ports`) or with an invalid `metadata.name`
- **When** POST is called
- **Then** the response MUST be 400 Bad Request with an RFC 7807 error body

##### AC-API-025: Create network -- unsupported routing_level

- Validates: REQ-API-110
- **Given** a request with `routing_level: application`
- **When** POST is called
- **Then** the response MUST be 400 Bad Request with an RFC 7807 error body indicating `application` routing is not supported in v1

##### AC-API-026: Create network -- service type inference

- Validates: REQ-API-080, REQ-API-240
- **Given** a request with `routing_level: network` and `provider_hints.kubernetes.node_ports` present
- **When** POST is called
- **Then** a LoadBalancer Service MUST be created with the specified node_ports
- And `kubernetes.type` in the response MUST be `"LoadBalancer"`

##### AC-API-030: List networks -- success

- Validates: REQ-API-120
- **Given** networks exist in the store
- **When** GET `/api/v1alpha1/networks` is called
- **Then** the response MUST be 200 OK
- And the body MUST conform to the `NetworkList` schema

##### AC-API-031: List networks -- pagination

- Validates: REQ-API-130
- **Given** more networks exist than `max_page_size`
- **When** GET is called with `?max_page_size=10`
- **Then** at most 10 networks MUST be returned
- And `next_page_token` MUST be present if more results exist

##### AC-API-032: List networks -- empty

- Validates: REQ-API-121
- **Given** no networks exist
- **When** GET `/api/v1alpha1/networks` is called
- **Then** the response MUST be 200 OK with an empty `results` array

##### AC-API-040: Get network -- success

- Validates: REQ-API-140
- **Given** a network with id `web-frontend-svc` exists
- **When** GET `/api/v1alpha1/networks/web-frontend-svc` is called
- **Then** the response MUST be 200 OK with the `Network` body including `kubernetes` runtime state

##### AC-API-041: Get network -- not found

- Validates: REQ-API-150
- **Given** no network with id `nonexistent-id` exists
- **When** GET is called with that `network_id`
- **Then** the response MUST be 404 Not Found with an RFC 7807 error body

##### AC-API-060: Delete network -- success

- Validates: REQ-API-210, REQ-API-211
- **Given** an existing network instance
- **When** DELETE `/api/v1alpha1/networks/{network_id}` is called
- **Then** the response MUST be 204 No Content with no body
- And subsequent GET for the same `network_id` MUST return 404

##### AC-API-061: Delete network -- not found

- Validates: REQ-API-220
- **Given** no network with the given `network_id` exists
- **When** DELETE is called
- **Then** the response MUST be 404 Not Found with an RFC 7807 error body

##### AC-API-070: Error response format

- Validates: REQ-API-230
- **Given** any error condition
- **When** an error response is returned
- **Then** the response MUST have `Content-Type: application/problem+json`
- And the body MUST contain at minimum `type` and `title` fields

##### AC-API-080: Provider hints -- acted on fields

- Validates: REQ-API-240
- **Given** a create request with `provider_hints.kubernetes.selector` and `provider_hints.kubernetes.node_ports`
- **When** the network is created
- **Then** the Service MUST use the specified selector and node_ports

##### AC-API-081: Provider hints -- passthrough fields ignored

- Validates: REQ-API-240
- **Given** a create request with additional `provider_hints` keys not listed in REQ-API-240
- **When** the network is created
- **Then** the request MUST succeed
- And unlisted hint keys MUST NOT affect Service creation

#### Dependencies

Depends on Topic 1 (HTTP Server) and Topic 4 (Kubernetes Integration & Store).

---

### 4.4 Kubernetes Integration & Store

#### Overview

Define a network storage interface that abstracts network persistence
operations. Implement it backed by Kubernetes Services. Manage Service lifecycle
in a single configured namespace. Apply DCM labels, infer Service type from
request fields, and validate preconditions for create.

Out of scope: creating Ingress, NetworkPolicy, Endpoints, or Pods.

#### Requirements -- Store Interface

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-STR-010 | The SP MUST define a `NetworkRepository` interface with Create, Get, List, Delete, and CheckHealth operations | MUST | DD-030 |
| REQ-STR-020 | The Create operation MUST return the created `Network` with all server-generated read-only fields populated | MUST | |
| REQ-STR-030 | The Create operation MUST return a conflict error if a network with the same `metadata.name` or instance ID already exists | MUST | |
| REQ-STR-040 | The Get operation MUST return the matching `Network` for a valid `network_id`, or a not-found error if no match exists | MUST | |
| REQ-STR-050 | The List operation MUST accept pagination parameters (`max_page_size`, `page_token`) and return a paginated `NetworkList` | MUST | |
| REQ-STR-060 | The List operation MUST default to `max_page_size=50` when not specified | MUST | |
| REQ-STR-080 | The Delete operation MUST delete the network matching the `network_id`, or return a not-found error if no match exists | MUST | |
| REQ-STR-090 | The store MUST define typed errors for not-found, conflict, and invalid-argument conditions so API handlers can map them to HTTP status codes | MUST | |
| REQ-STR-100 | CheckHealth MUST return an error when the backing Kubernetes API is unreachable | MUST | REQ-HLT-050, REQ-HLT-070 |

> **Note:** HTTP handlers depend on `NetworkRepository` only; the Kubernetes
> store (`K8sNetworkStore`) implements this interface.

#### Requirements -- Kubernetes Integration

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-K8S-010 | The store MUST use `k8s.io/client-go` against one cluster API | MUST | |
| REQ-K8S-020 | All managed Services MUST be created in the configured namespace | MUST | DD-030 |
| REQ-K8S-030 | Kubeconfig MAY be supplied via `SP_K8S_KUBECONFIG`; in-cluster config MUST be used when unset | MUST | |
| REQ-K8S-040 | Every created Service MUST include DCM labels (see 5.1) | MUST | |
| REQ-K8S-050 | Instance lookup MUST use the `dcm.project/dcm-instance-id` label | MUST | |
| REQ-K8S-060 | List/get operations MUST scope to DCM-managed Services (`managed-by=dcm`, `dcm-service-type=network`) | MUST | |
| REQ-K8S-070 | The SP MUST infer the Kubernetes Service type from `routing_level` + `node_ports` presence per the Service Type Inference table | MUST | DD-020 |
| REQ-K8S-080 | `spec.ports` MUST map to Service `spec.ports` with `name`, `protocol`, `port`, `targetPort` fields | MUST | |
| REQ-K8S-090 | `provider_hints.kubernetes.selector` MUST map to Service `spec.selector` | MUST | |
| REQ-K8S-100 | `provider_hints.kubernetes.cluster_ip` MUST map to Service `spec.clusterIP` | MUST | |
| REQ-K8S-110 | `provider_hints.kubernetes.node_ports` MUST map to the `nodePort` field on matching Service ports | MUST | |
| REQ-K8S-150 | DELETE MUST issue a Kubernetes delete on the Service | MUST | |
| REQ-K8S-160 | User-supplied `metadata.labels` MUST NOT use reserved DCM label keys | MUST | 5.1 |
| REQ-K8S-170 | Network status in API responses MUST be derived from Service type and state (see 4.5 status mapping) | MUST | |
| REQ-K8S-180 | List operations MUST support pagination over DCM-managed Services in the configured namespace, mapping results to `page_token` / `next_page_token` | MUST | |
| REQ-K8S-190 | Get and Delete MUST return a conflict error when multiple Services match the same `dcm-instance-id` label | MUST | |
| REQ-K8S-200 | The SP MUST support authentication via kubeconfig file when `SP_K8S_KUBECONFIG` is set | MUST | |
| REQ-K8S-210 | The SP MUST support in-cluster service account authentication when `SP_K8S_KUBECONFIG` is unset | MUST | |

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| kubernetes.namespace | SP_K8S_NAMESPACE | default | Namespace for all Services |
| kubernetes.kubeconfig | SP_K8S_KUBECONFIG | (in-cluster) | Path to kubeconfig |

#### Acceptance Criteria -- Store Interface

##### AC-STR-010: Create operation populates read-only fields

- Validates: REQ-STR-020
- **Given** a valid `Network.spec` and instance ID are passed to Create
- **When** the operation succeeds
- **Then** the returned `Network` MUST have `id`, `path`, `status`, `metadata.namespace`, and `kubernetes` fields populated

##### AC-STR-020: Create conflict detection

- Validates: REQ-STR-030
- **Given** a network with `metadata.name` `web-svc` already exists
- **When** Create is called with another network with `metadata.name` `web-svc`
- **Then** a conflict error MUST be returned

##### AC-STR-030: Get operation -- found

- Validates: REQ-STR-040
- **Given** a network with id `abc-123` exists
- **When** Get is called with `network_id` `abc-123`
- **Then** the matching `Network` MUST be returned

##### AC-STR-040: Get operation -- not found

- Validates: REQ-STR-040
- **Given** no network with id `xyz-999` exists
- **When** Get is called with `network_id` `xyz-999`
- **Then** a not-found error MUST be returned

##### AC-STR-050: List operation -- first page

- Validates: REQ-STR-050
- **Given** more than 50 networks exist and `max_page_size` is 50
- **When** List is called
- **Then** the first page MUST contain 50 networks
- And `next_page_token` MUST be non-empty

##### AC-STR-055: List operation -- subsequent page

- Validates: REQ-STR-050
- **Given** a valid `page_token` from a previous List call
- **When** List is called with that `page_token`
- **Then** the next page of results MUST be returned

##### AC-STR-060: List default page size

- Validates: REQ-STR-060
- **Given** no `max_page_size` is provided
- **When** List is called
- **Then** at most 50 networks MUST be returned

##### AC-STR-080: Delete operation

- Validates: REQ-STR-080
- **Given** a network with id `abc-123` exists
- **When** Delete is called with `network_id` `abc-123`
- **Then** the network MUST be removed
- And subsequent Get(`abc-123`) MUST return not-found

##### AC-STR-090: Error type -- not found

- Validates: REQ-STR-090
- **Given** a not-found condition occurs in the store
- **When** the error is returned
- **Then** the error MUST be distinguishable as a not-found error

##### AC-STR-100: Error type -- conflict

- Validates: REQ-STR-090
- **Given** a conflict condition occurs in the store
- **When** the error is returned
- **Then** the error MUST be distinguishable as a conflict error

##### AC-STR-110: Error type -- invalid argument

- Validates: REQ-STR-090
- **Given** an invalid-argument condition occurs (e.g., malformed `page_token`)
- **When** the error is returned
- **Then** the error MUST be distinguishable as an invalid-argument error

#### Acceptance Criteria -- Kubernetes Integration

##### AC-K8S-010: Service created with DCM labels

- Validates: REQ-K8S-040
- **Given** a create request with instance ID `abc-123`
- **When** the Service is persisted
- **Then** labels MUST include `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id=abc-123`, `dcm.project/dcm-service-type=network`

##### AC-K8S-030: Service type inference

- Validates: REQ-K8S-070
- **Given** a create request with `routing_level: network` and no `node_ports`
- **When** the Service is created
- **Then** the Service type MUST be `LoadBalancer`
- And when `routing_level` is omitted and `node_ports` are present, the Service type MUST be `NodePort`
- And when both are omitted, the Service type MUST be `ClusterIP`

##### AC-K8S-050: Port mapping

- Validates: REQ-K8S-080
- **Given** a create request with `ports: [{name: "http", protocol: "TCP", port: 80, target_port: 8080}]`
- **When** the Service is created
- **Then** the Service MUST have `spec.ports[0]` with `name: http`, `protocol: TCP`, `port: 80`, `targetPort: 8080`

##### AC-K8S-060: Selector mapping

- Validates: REQ-K8S-090
- **Given** a create request with `provider_hints.kubernetes.selector: {app: "web"}`
- **When** the Service is created
- **Then** the Service MUST have `spec.selector: {app: "web"}`

##### AC-K8S-070: ClusterIP mapping

- Validates: REQ-K8S-100
- **Given** a create request with `provider_hints.kubernetes.cluster_ip: "None"`
- **When** the Service is created
- **Then** the Service MUST have `spec.clusterIP: "None"` (headless)

##### AC-K8S-080: NodePort mapping

- Validates: REQ-K8S-110
- **Given** a create request with `provider_hints.kubernetes.node_ports: {"http": 30080}`
- **When** the Service is created
- **Then** the port named `http` MUST have `nodePort: 30080`

##### AC-K8S-110: Kubernetes authentication -- kubeconfig

- Validates: REQ-K8S-200
- **Given** `SP_K8S_KUBECONFIG` points to a valid kubeconfig file
- **When** the SP initializes the K8s client
- **Then** the client MUST authenticate using the kubeconfig credentials

##### AC-K8S-120: Kubernetes authentication -- in-cluster

- Validates: REQ-K8S-210
- **Given** `SP_K8S_KUBECONFIG` is not set and the SP runs inside a Kubernetes cluster
- **When** the SP initializes the K8s client
- **Then** the client MUST authenticate using the in-cluster service account

##### AC-K8S-130: List pagination

- Validates: REQ-K8S-180
- **Given** more DCM-managed Services exist than `max_page_size`
- **When** List is called
- **Then** pagination MUST work correctly via `page_token` / `next_page_token`

##### AC-K8S-140: Namespace for all Services

- Validates: REQ-K8S-020
- **Given** SP configuration has `namespace=production`
- **When** any Service is created
- **Then** the Service MUST be in the `production` namespace

##### AC-K8S-150: Status mapping -- ready

- Validates: REQ-K8S-170
- **Given** a ClusterIP or NodePort Service exists
- **When** Get is called
- **Then** `status` MUST be `READY`

##### AC-K8S-155: Status mapping -- pending

- Validates: REQ-K8S-170
- **Given** a LoadBalancer Service exists with empty `.status.loadBalancer.ingress[]`
- **When** Get is called
- **Then** `status` MUST be `PENDING`

##### AC-K8S-170: Multiple Service conflict

- Validates: REQ-K8S-190
- **Given** two Services share the same `dcm.project/dcm-instance-id` label
- **When** Get or Delete is called with that instance ID
- **Then** a conflict error MUST be returned

#### Dependencies

None -- independently deliverable (Store Interface and K8s Integration are co-delivered).

---

### 4.5 Resource Status Monitoring & Reporting

#### Overview

Watch Services via a `SharedIndexInformer`. Debounce rapid updates. Map
Kubernetes state to portable `NetworkStatus` and publish CloudEvents to NATS
subject `dcm.network` for consumption by the control-plane SP manager.

Out of scope: JetStream publish semantics on the SP side (stream management is
the consumer's responsibility; the SP publishes to a plain NATS subject),
historical event replay.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-MON-010 | The SP MUST watch Service resources in the configured namespace using a `SharedIndexInformer` | MUST | |
| REQ-MON-020 | The informer MUST filter Services using label selector `dcm.project/managed-by=dcm,dcm.project/dcm-service-type=network` | MUST | |
| REQ-MON-030 | The informer MUST maintain a secondary index on the `dcm.project/dcm-instance-id` label to enable fast lookups | MUST | |
| REQ-MON-040 | Status reconciliation MUST follow the status mapping table | MUST | REQ-MON-120 |
| REQ-MON-050 | When a Service object no longer exists for a previously tracked instance, the status MUST be `DELETED` | MUST | |
| REQ-MON-060 | Status changes MUST be published to NATS subject `dcm.network` | MUST | |
| REQ-MON-070 | Events MUST use CloudEvents v1.0 format. Required attributes (`id`, `source`, `type`, `subject`, `specversion`, `datacontenttype`) MUST be set | MUST | |
| REQ-MON-080 | CloudEvent `type` MUST be `dcm.status.network` | MUST | |
| REQ-MON-090 | CloudEvent `source` MUST be `dcm/providers/{providerName}` | MUST | |
| REQ-MON-095 | CloudEvent `subject` MUST be `dcm.network` | MUST | |
| REQ-MON-100 | CloudEvent `datacontenttype` MUST be `application/json` | MUST | |
| REQ-MON-110 | The data payload MUST include `id` (DCM instance ID), `status` (DCM status string), and `message` (human-readable description) | MUST | |
| REQ-MON-120 | `status` MUST be one of: `PENDING`, `READY`, `DELETED` | MUST | |
| REQ-MON-130 | The SP MUST debounce rapid status oscillations to avoid flooding the messaging system | MUST | |
| REQ-MON-140 | Debounce MUST be applied per instance ID so changes for one network do not suppress events for another | MUST | |
| REQ-MON-150 | The instance ID MUST be extracted from the `dcm.project/dcm-instance-id` label on the Service | MUST | |
| REQ-MON-160 | The informer MUST be started as an asynchronous background task after the HTTP server is ready | MUST | |
| REQ-MON-161 | The informer MUST be stopped during graceful shutdown | MUST | |
| REQ-MON-170 | The informer MUST periodically re-reconcile status for all tracked Services at the configured resync interval | MUST | |
| REQ-MON-175 | On startup, after the informer cache has completed initial synchronization, the SP MUST publish a status CloudEvent for every existing DCM-managed Service | MUST | |
| REQ-MON-190 | The informer MUST automatically reconnect after API server disconnection and resume processing events without manual intervention | MUST | |
| REQ-MON-200 | Status event publishing MUST be decoupled from the transport mechanism via a `StatusPublisher` interface | MUST | |
| REQ-MON-210 | Status event publishing MUST retry with exponential backoff on transient NATS failures, up to a configurable maximum number of attempts | MUST | |
| REQ-MON-220 | When NATS is unavailable, the SP MUST log the failure and continue operating without crashing. The NATS connection MUST use unlimited reconnection attempts with disconnect/reconnect event logging | MUST | |

**Status mapping (REQ-MON-120):**

| DCM `NetworkStatus` | Kubernetes condition |
|---|---|
| `PENDING` | Service exists AND `.spec.type` = LoadBalancer AND `.status.loadBalancer.ingress[]` is empty |
| `READY` | Service exists AND `.spec.type` = LoadBalancer AND `.status.loadBalancer.ingress[]` has entries |
| `READY` | Service exists AND `.spec.type` = NodePort |
| `READY` | Service exists AND `.spec.type` = ClusterIP |
| `DELETED` | Service object no longer exists |

> **Field notes:** `.status.loadBalancer.ingress[]` can contain multiple entries.
> Each entry can have either `.ip` (e.g., GKE, MetalLB) or `.hostname` (e.g.,
> AWS ELB). LoadBalancer is `READY` when at least one entry exists. ClusterIP
> services with `.spec.clusterIP = "None"` are headless and immediately `READY`.

> **Rationale:** Services are declarative networking abstractions -- they define
> routing rules and are functional once created, regardless of endpoint
> availability. ClusterIP and NodePort services are immediately `READY`. Empty
> endpoints are normal during initial creation, rolling updates, scaling to zero,
> temporary pod failures, and headless services.

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| nats.url | SP_NATS_URL | (required) | NATS server URL |
| provider.name | SP_NAME | (required) | Provider name for CloudEvents `source` |
| monitoring.debounceMs | SP_MONITOR_DEBOUNCE_MS | 500 | Status debounce interval |
| monitoring.resyncPeriod | SP_MONITOR_RESYNC_PERIOD | 10m | Informer resync period |

#### Acceptance Criteria

##### AC-MON-010: Service informer created

- Validates: REQ-MON-010
- **Given** the SP starts with valid K8s credentials
- **When** the monitoring subsystem initializes
- **Then** a `SharedIndexInformer` MUST be created for Services in the configured namespace

##### AC-MON-020: Label selector filtering

- Validates: REQ-MON-020
- **Given** the informer is running
- **When** Services are watched
- **Then** only Services with `dcm.project/managed-by=dcm` and `dcm.project/dcm-service-type=network` MUST be observed

##### AC-MON-030: dcm-instance-id secondary index

- Validates: REQ-MON-030
- **Given** the informer receives a Service event
- **When** the Service has label `dcm.project/dcm-instance-id=abc-123`
- **Then** the Service MUST be indexable by instance ID `abc-123`

##### AC-MON-040: Status mapping -- pending

- Validates: REQ-MON-040, REQ-MON-120
- **Given** a LoadBalancer Service exists with empty `.status.loadBalancer.ingress[]`
- **When** status is reconciled
- **Then** the DCM status MUST be `PENDING`

##### AC-MON-050: Status mapping -- ready

- Validates: REQ-MON-040, REQ-MON-120
- **Given** a ClusterIP or NodePort Service exists, or a LoadBalancer Service has entries in `.status.loadBalancer.ingress[]`
- **When** status is reconciled
- **Then** the DCM status MUST be `READY`

##### AC-MON-060: Status mapping -- deleted

- Validates: REQ-MON-050
- **Given** a Service delete event is processed and the object no longer exists
- **When** status is reconciled
- **Then** the DCM status MUST be `DELETED`

##### AC-MON-070: CloudEvents format

- Validates: REQ-MON-070, REQ-MON-080, REQ-MON-090, REQ-MON-095, REQ-MON-100, REQ-MON-110
- **Given** a status change is detected for instance `abc-123` with provider name `k8s-network-sp`
- **When** the event is published
- **Then** the CloudEvent MUST include:
  - `specversion`: `"1.0"`
  - `id`: unique event identifier (non-empty)
  - `source`: `"dcm/providers/k8s-network-sp"`
  - `type`: `"dcm.status.network"`
  - `subject`: `"dcm.network"`
  - `datacontenttype`: `"application/json"`
  - `data`: `{"id": "abc-123", "status": "<DCM_STATUS>", "message": "<description>"}`

##### AC-MON-080: NATS publishing

- Validates: REQ-MON-060
- **Given** a status change is detected
- **When** the event is published
- **Then** it MUST be published to NATS subject `dcm.network`

##### AC-MON-090: Debounce logic

- Validates: REQ-MON-130
- **Given** multiple status changes occur within the debounce interval for the same instance
- **When** events are processed
- **Then** only the last status within the debounce window MUST be published

##### AC-MON-091: Per-instance debounce isolation

- Validates: REQ-MON-140
- **Given** status changes occur within the debounce interval for two different instances
- **When** events are processed
- **Then** each instance's events MUST be debounced independently

##### AC-MON-100: Instance ID extraction

- Validates: REQ-MON-150
- **Given** a Service event is received
- **When** the handler processes it
- **Then** the `dcm.project/dcm-instance-id` label value MUST be used as the instance ID

##### AC-MON-110: Informer lifecycle -- startup

- Validates: REQ-MON-160
- **Given** the HTTP server has started
- **When** the monitoring subsystem starts
- **Then** the informer MUST run as an asynchronous background task

##### AC-MON-120: Informer lifecycle -- shutdown

- Validates: REQ-MON-161
- **Given** the SP receives a shutdown signal
- **When** graceful shutdown begins
- **Then** the informer MUST be stopped

##### AC-MON-130: Cache resync

- Validates: REQ-MON-170
- **Given** the informer is running
- **When** the resync period elapses (default: 10 minutes)
- **Then** status reconciliation MUST be re-evaluated for every Service in the local cache

##### AC-MON-140: Initial status sync on startup

- Validates: REQ-MON-175
- **Given** the SP starts or restarts with existing DCM-managed Services
- **When** the informer cache has completed initial synchronization
- **Then** a status CloudEvent MUST be published for each existing Service
- And debounce logic (REQ-MON-130) MUST apply to these initial events

##### AC-MON-160: Informer resilience

- Validates: REQ-MON-190
- **Given** the API server connection is interrupted
- **When** the API server becomes available again
- **Then** the informer MUST automatically reconnect and resume event processing

##### AC-MON-170: Decoupled status publishing

- Validates: REQ-MON-200
- **Given** the status publishing subsystem
- **When** status events are published
- **Then** publishing MUST be decoupled from the transport via `StatusPublisher`

##### AC-MON-180: Publish retry on failure

- Validates: REQ-MON-210
- **Given** a transient NATS failure occurs during event publishing
- **When** the publisher retries
- **Then** retries MUST use exponential backoff up to a configurable maximum

##### AC-MON-190: NATS failure handling

- Validates: REQ-MON-220
- **Given** NATS is unavailable
- **When** the SP attempts to publish a status event
- **Then** the failure MUST be logged at ERROR level
- And the SP MUST continue serving HTTP requests without crashing

#### Dependencies

Depends on Topic 4 (Kubernetes Integration & Store).

---

### 4.6 DCM Registration

#### Overview

Self-register with the DCM Service Provider Registry on startup. Registration
runs asynchronously with exponential backoff and does not block HTTP server
startup. The registered endpoint suffix is derived from the OpenAPI POST path
(`/api/v1alpha1/networks`).

Out of scope: de-registration on shutdown, registration status health check
integration, provider capability updates post-registration.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-REG-010 | The SP MUST register on startup via `POST {DCM_REGISTRATION_URL}/providers` | MUST | |
| REQ-REG-020 | The registration payload MUST include `name`, `service_type`, `endpoint`, `operations`, `schema_version`, and optionally `display_name`, `metadata.region_code`/`metadata.zone` | MUST | |
| REQ-REG-030 | Registration MUST execute asynchronously | MUST | |
| REQ-REG-031 | Registration MUST NOT block server startup | MUST | |
| REQ-REG-040 | `service_type` MUST be `"network"` | MUST | |
| REQ-REG-041 | `operations` MUST include `CREATE`, `READ`, and `DELETE` | MUST | UPDATE is out of v1 scope |
| REQ-REG-042 | `endpoint` MUST be `{SP_ENDPOINT}/api/v1alpha1/networks` | MUST | Derived from OpenAPI POST path; DD-010 |
| REQ-REG-043 | `schema_version` MUST be `v1alpha1` | MUST | |
| REQ-REG-050 | Registration MUST retry with exponential backoff on failure with a maximum backoff interval. Non-retryable errors (4xx client errors) MUST stop retries immediately | MUST | |
| REQ-REG-060 | Registration failures MUST be logged | MUST | |
| REQ-REG-061 | Registration failures MUST NOT cause the SP to exit | MUST | |
| REQ-REG-070 | Registration MUST be idempotent: re-registration on restart updates the existing entry (not duplicated) | MUST | |
| REQ-REG-080 | The SP MUST use the official DCM provider API client library | MUST | |

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| dcm.registrationUrl | DCM_REGISTRATION_URL | (required) | DCM SP registration base URL |
| provider.name | SP_NAME | (required) | Provider name |
| provider.displayName | SP_DISPLAY_NAME | (optional) | Human-readable name |
| provider.endpoint | SP_ENDPOINT | (required) | Externally reachable SP base URL |
| provider.region | SP_REGION | (optional) | Region metadata |
| provider.zone | SP_ZONE | (optional) | Zone metadata |

#### Acceptance Criteria

##### AC-REG-010: Self-registration on startup

- Validates: REQ-REG-010
- **Given** the SP starts with valid DCM registration configuration
- **When** the HTTP server is ready
- **Then** a registration request MUST be sent to the DCM SP API

##### AC-REG-020: Registration payload

- Validates: REQ-REG-020, REQ-REG-040, REQ-REG-041, REQ-REG-042, REQ-REG-043
- **Given** provider configuration is set
- **When** `BuildPayload` constructs the registration request
- **Then** it MUST include:
  - `name`: configured provider name
  - `service_type`: `"network"`
  - `schema_version`: `"v1alpha1"`
  - `display_name`: configured display name (if set)
  - `endpoint`: `{provider.endpoint}/api/v1alpha1/networks`
  - `operations`: `["CREATE", "READ", "DELETE"]`
  - `metadata.region_code`: configured region (if set)
  - `metadata.zone`: configured zone (if set)

##### AC-REG-030: Non-blocking registration

- Validates: REQ-REG-030, REQ-REG-031
- **Given** the HTTP server has started
- **When** registration is initiated
- **Then** the server MUST already be accepting HTTP requests
- And registration MUST run concurrently

##### AC-REG-040: Exponential backoff on failure

- Validates: REQ-REG-050
- **Given** the DCM registration endpoint is unreachable
- **When** a registration attempt fails
- **Then** the SP MUST retry with exponential backoff
- And a maximum backoff interval MUST be enforced

##### AC-REG-045: Registration stops on 4xx

- Validates: REQ-REG-050
- **Given** the DCM registry returns a 4xx status code
- **When** a registration attempt receives this response
- **Then** the SP MUST NOT retry
- And MUST log the error at ERROR level
- And MUST continue running and serving requests

##### AC-REG-050: Registration failure logging

- Validates: REQ-REG-060, REQ-REG-061
- **Given** registration fails after multiple retries
- **When** the error is handled
- **Then** the error MUST be logged at an appropriate level
- And the SP MUST continue running and serving requests

##### AC-REG-060: Idempotent re-registration

- Validates: REQ-REG-070
- **Given** the SP was previously registered with DCM
- **When** the SP restarts and re-registers
- **Then** the existing registration MUST be updated (not duplicated)

##### AC-REG-070: Registration client library

- Validates: REQ-REG-080
- **Given** the registration subsystem is implemented
- **When** the registration request is sent
- **Then** it MUST use the official DCM service provider API client library

#### Dependencies

Depends on Topic 1 (HTTP Server).

---

## 5. Cross-Cutting Concerns

### 5.1 DCM Labels

Reserved label keys (MUST NOT be set by callers in `metadata.labels`):

| Label | Value |
|-------|-------|
| `dcm.project/managed-by` | `dcm` |
| `dcm.project/dcm-instance-id` | DCM instance ID (AEP-122) |
| `dcm.project/dcm-service-type` | `network` |

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-LBL-010 | All Kubernetes Services managed by this SP MUST carry the DCM labels: `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id={networkId}`, `dcm.project/dcm-service-type=network` | MUST | REQ-K8S-040 |

#### Acceptance Criteria

##### AC-XC-LBL-010: DCM labels applied to all Services

- Validates: REQ-XC-LBL-010
- **Given** any Service is created by the SP
- **When** the Service is applied to the cluster
- **Then** it MUST carry all three DCM labels with correct values

**Related requirements:** REQ-K8S-040, REQ-K8S-050, REQ-K8S-160

### 5.2 Resource Identity

| Field | Source |
|-------|--------|
| `id` / `network_id` | DCM instance ID (`dcm-instance-id` label), AEP-122 format |
| `metadata.name` | Kubernetes Service name (DNS-1123 subdomain) |
| `path` | `networks/{network_id}` |

Client may supply `?id=` on POST (AEP-122); otherwise the server generates an ID.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-ID-010 | Two identifiers MUST be used for network resources: `id` (DCM instance ID in AEP-122 format, used in URL paths and stored as `dcm.project/dcm-instance-id` label) and `metadata.name` (used as the Kubernetes Service name) | MUST | |
| REQ-XC-ID-020 | Conflict detection on create MUST be based on `metadata.name`, not `id`. Both uniqueness constraints apply independently | MUST | REQ-API-100 |

#### Acceptance Criteria

##### AC-XC-ID-010: Dual identifier usage

- Validates: REQ-XC-ID-010
- **Given** a network is created with id `web-frontend-svc` and `metadata.name` `web-frontend`
- **When** the Service is stored
- **Then** `id` MUST be used in URL paths (`/networks/{network_id}`) and as the `dcm.project/dcm-instance-id` label
- And `metadata.name` MUST be the Kubernetes Service name `web-frontend`

##### AC-XC-ID-020: Conflict detection based on metadata.name

- Validates: REQ-XC-ID-020
- **Given** a network with `metadata.name` `web-frontend` already exists
- **When** a new network with a different `id` but the same `metadata.name` `web-frontend` is created
- **Then** the request MUST be rejected with a conflict error

### 5.3 Error Handling

All API errors use RFC 7807 (`application/problem+json`). The `type` field uses
the enumerated codes in the OpenAPI `Error` schema.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-ERR-010 | All HTTP error responses MUST conform to RFC 7807 using the `Error` schema defined in the OpenAPI spec | MUST | REQ-API-230 |
| REQ-XC-ERR-020 | Error responses MUST set `Content-Type: application/problem+json` | MUST | |
| REQ-XC-ERR-030 | Error responses SHOULD include `detail` and `instance` fields. The `instance` field SHOULD be the request URI | SHOULD | |
| REQ-XC-ERR-040 | Error responses for INTERNAL errors MUST NOT expose implementation details such as stack traces, panic messages, raw dependency error strings, file paths, or memory addresses | MUST | REQ-HTTP-070 |

#### Acceptance Criteria

##### AC-XC-ERR-010: RFC 7807 compliance

- Validates: REQ-XC-ERR-010
- **Given** any error condition in the API
- **When** an error response is returned
- **Then** the body MUST conform to the RFC 7807 `Error` schema with at minimum `type` and `title` fields

##### AC-XC-ERR-020: Error content type

- Validates: REQ-XC-ERR-020
- **Given** any error response
- **When** the response is sent
- **Then** the `Content-Type` header MUST be `application/problem+json`

##### AC-XC-ERR-030: Instance field for tracing

- Validates: REQ-XC-ERR-030
- **Given** any error condition
- **When** the error response is returned
- **Then** the `instance` field SHOULD be set to the request URI

##### AC-XC-ERR-040: No implementation detail leakage

- Validates: REQ-XC-ERR-040
- **Given** an internal error occurs (unexpected store error, panic, or validation edge case)
- **When** the error response is returned
- **Then** the `detail` field MUST contain a generic message
- And the response MUST NOT contain stack traces, file paths, memory addresses, or raw internal error messages

### 5.4 Logging

Structured logging via `log/slog`. Registration failures, Kubernetes API errors,
NATS disconnects, and HTTP panics MUST be logged at appropriate levels.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-LOG-010 | Structured logging MUST be used throughout the application | MUST | |
| REQ-XC-LOG-020 | Log levels MUST follow the defined convention: ERROR (unrecoverable failures), WARN (recoverable issues), INFO (lifecycle events), DEBUG (detailed data) | MUST | |

**Log level convention:**

| Level | Usage |
|-------|-------|
| ERROR | Unrecoverable failures, K8s API errors, NATS publish failures |
| WARN | Recoverable issues, registration retries, health check failures |
| INFO | Lifecycle events, network create/delete operations, HTTP request logging |
| DEBUG | Detailed request/response data, informer events |

#### Acceptance Criteria

##### AC-XC-LOG-010: Structured logging

- Validates: REQ-XC-LOG-010
- **Given** any operation occurs in the SP
- **When** the operation is logged
- **Then** the log output MUST use structured logging format

##### AC-XC-LOG-020: Log level usage

- Validates: REQ-XC-LOG-020
- **Given** different types of events occur
- **When** they are logged
- **Then** ERROR, WARN, INFO, and DEBUG levels MUST be used according to the defined convention

### 5.5 Configuration Management

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-CFG-010 | All configuration MUST be loadable from environment variables | MUST | REQ-HTTP-050 |
| REQ-XC-CFG-020 | The SP MUST fail fast on startup when required configuration values are absent or empty, returning an error before starting any subsystem | MUST | |

**Required configuration (v1):** `SP_NAME`, `SP_ENDPOINT`, `DCM_REGISTRATION_URL`.
`SP_NATS_URL` is required when the monitoring subsystem is enabled (Topic 5).

#### Acceptance Criteria

##### AC-XC-CFG-010: Environment variable configuration

- Validates: REQ-XC-CFG-010
- **Given** any configuration value
- **When** the corresponding environment variable is set
- **Then** the SP MUST use the value from the environment variable

##### AC-XC-CFG-020: Fail-fast on missing required config

- Validates: REQ-XC-CFG-020
- **Given** a required config value (`SP_NAME`, `SP_ENDPOINT`, or `DCM_REGISTRATION_URL`) is absent or empty
- **When** the SP starts
- **Then** the SP MUST return an error identifying the missing field
- And MUST exit before starting the HTTP server or any subsystem

---

## 6. Consolidated Configuration Reference

| Config Key | Env Var | Default | Required | Topic |
|------------|---------|---------|----------|-------|
| server.address | SP_SERVER_ADDRESS | :8080 | No | 1 |
| server.shutdownTimeout | SP_SERVER_SHUTDOWN_TIMEOUT | 15s | No | 1 |
| server.readTimeout | SP_SERVER_READ_TIMEOUT | 15s | No | 1 |
| server.writeTimeout | SP_SERVER_WRITE_TIMEOUT | 15s | No | 1 |
| server.idleTimeout | SP_SERVER_IDLE_TIMEOUT | 60s | No | 1 |
| server.requestTimeout | SP_SERVER_REQUEST_TIMEOUT | 30s | No | 1 |
| kubernetes.namespace | SP_K8S_NAMESPACE | default | No | 4 |
| kubernetes.kubeconfig | SP_K8S_KUBECONFIG | (in-cluster) | No | 4 |
| nats.url | SP_NATS_URL | - | Yes | 5 |
| monitoring.debounceMs | SP_MONITOR_DEBOUNCE_MS | 500 | No | 5 |
| monitoring.resyncPeriod | SP_MONITOR_RESYNC_PERIOD | 10m | No | 5 |
| dcm.registrationUrl | DCM_REGISTRATION_URL | - | Yes | 6 |
| provider.name | SP_NAME | - | Yes | 6 |
| provider.displayName | SP_DISPLAY_NAME | - | No | 6 |
| provider.endpoint | SP_ENDPOINT | - | Yes | 6 |
| provider.region | SP_REGION | - | No | 6 |
| provider.zone | SP_ZONE | - | No | 6 |

---

## 7. Design Decisions

### DD-010: Health endpoint path

**Decision:** Health is at `GET /api/v1alpha1/networks/health` (resource-relative,
for DCM health check integration which derives the health URL as
`registered_endpoint + '/health'`). Network API is under
`/api/v1alpha1/networks`. Registration endpoint is
`{SP_ENDPOINT}/api/v1alpha1/networks`.

**Rationale:** SPRM's `healthcheck/monitor.go` constructs health URLs as
`endpoint + "/health"`, and the registered endpoint is
`{base}/api/v1alpha1/networks`. Follows the established DCM SP convention
of resource-relative health paths.

**Related requirements:** REQ-HTTP-020, REQ-HLT-010, REQ-REG-042

### DD-020: Service type inference, not configuration

**Decision:** The Kubernetes Service type (ClusterIP, NodePort, LoadBalancer) is
inferred per-request from `routing_level` + `node_ports` presence. There is no
`DefaultServiceType` provider-level configuration.

**Rationale:** The service type is a property of each network request, not a
provider default. The `routing_level` field expresses user-facing intent
(transport-level vs application-level), and `node_ports` presence further
refines the inferred type. No provider-level default is needed.

**Related requirements:** REQ-K8S-070, REQ-API-080, REQ-API-240

### DD-030: Single namespace per SP instance

**Decision:** All Services are created in `SP_K8S_NAMESPACE`. Per-service
namespace override is out of scope for v1.

**Rationale:** Simplifies RBAC, informers, and quota checks. Multiple SP
registrations can target the same cluster with different namespaces.

**Related requirements:** REQ-K8S-020

### DD-040: Health response schema

**Decision:** Health response uses `status: "healthy"/"unhealthy"` with AEP
fields (`type`, `path`) plus operational fields (`version`, `uptime`). The SP
checks backing K8s cluster liveness via
`client.Discovery().ServerVersion()`.

**Rationale:** The `type` field value
`k8s-network-service-provider.dcm.io/health` uniquely identifies this SP's
health responses in multi-provider environments.

**Related requirements:** REQ-HLT-020

### DD-050: Status values PENDING/READY/DELETED

**Decision:** Network services use three status values: `PENDING` (LoadBalancer
waiting for external IP), `READY` (operational), `DELETED` (not found).

**Rationale:** Kubernetes Services are declarative networking abstractions.
ClusterIP and NodePort services are functional immediately upon creation -- only
LoadBalancer services have a transitional state (waiting for external IP
assignment). Services do not have failure phases and deletion is immediate, so
only three states are needed.

**Related requirements:** REQ-MON-120, REQ-API-080

### DD-060: DCM three-state health model

**Decision:** Health reports `healthy` or `unhealthy` based solely on Kubernetes
API server reachability (version discovery). HTTP status remains 200 OK in both
cases; DCM interprets the `status` field per the
service-provider-health-check enhancement.

**Rationale:** Aligns with DCM control-plane polling semantics. NATS
connectivity is intentionally excluded from health in v1.

**Related requirements:** REQ-HLT-020, REQ-HLT-050, REQ-HLT-060

---

## 8. Requirement ID Index

| Prefix | Topic |
|--------|-------|
| REQ-HTTP-* | HTTP Server |
| REQ-HLT-* | Health Service |
| REQ-API-* | Network API Handlers |
| REQ-STR-* | Store Interface (4.4) |
| REQ-K8S-* | Kubernetes Integration (4.4) |
| REQ-MON-* | Status Monitoring & Reporting |
| REQ-REG-* | DCM Registration |
| REQ-XC-* | Cross-Cutting Concerns (5) |
