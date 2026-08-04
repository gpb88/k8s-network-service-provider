# Test Plan: K8s Network SP — Unit Tests

## Overview

- **Related Spec:** .ai/specs/k8s-network-sp.spec.md
- **Framework:** Ginkgo v2 + Gomega
- **Created:** 2026-07-28
- **Last Updated:** 2026-08-04

Unit tests verify individual components in isolation. All external dependencies
(Kubernetes client, NATS, DCM control-plane) are replaced with mocks or fakes.
Tests use `httptest.NewRecorder` for handler tests and direct function calls for
pure logic.

**Field naming:** SP OpenAPI and catalog `network` service type both use
`provider_hints.kubernetes` with snake_case keys (`selector`, `cluster_ip`,
`node_ports`).

**No real external services:**
- No real Kubernetes cluster
- No real NATS
- No real DCM control-plane

**Mocking approach:**
- Mock Kubernetes client: `k8s.io/client-go/kubernetes/fake`
- Mock NATS publisher: custom mock interface
- HTTP handlers: `httptest.NewRecorder`

---

## 1 · Configuration

> **Suggested Ginkgo structure:** `Describe("Configuration")`

### TC-U001: Load configuration from environment variables

- **Priority:** High
- **Type:** Unit
- **Given:** Environment variables are set:
  - `SP_SERVER_ADDRESS=":9090"`
  - `SP_K8S_NAMESPACE="team-a"`
- **When:** Config is loaded
- **Then:**
  - `server.address = ":9090"`
  - `kubernetes.namespace = "team-a"`

### TC-U002: Default values applied when no config specified

- **Priority:** Medium
- **Type:** Unit
- **Given:** Only required environment variables are set (SP_NAME, SP_ENDPOINT,
  DCM_REGISTRATION_URL)
- **When:** Config is loaded
- **Then:**
  - `server.address` defaults to `":8080"`
  - `kubernetes.namespace` defaults to `"default"`
  - `server.shutdownTimeout` defaults to `15s`

### TC-U003: Validate required configuration fields

- **Priority:** High
- **Type:** Unit
- **Given:** Config is missing required field (SP_NAME, SP_ENDPOINT, or
  DCM_REGISTRATION_URL)
- **When:** Config validation runs
- **Then:** Error is returned indicating missing required field

### TC-U004: Namespace configuration validation

- **Priority:** High
- **Type:** Unit
- **Given:** Namespace is set to invalid value (e.g., contains uppercase, special
  chars)
- **When:** Config validation runs
- **Then:** Error is returned with invalid namespace message

---

## 2 · Service Spec Building

> **Phase:** Network CRUD follow-up (not in health+registration PR)
> **Suggested Ginkgo structure:** `Describe("Service Spec Building")`

### TC-U010: Build K8s Service from minimal DCM request

- **Priority:** High
- **Type:** Unit
- **Given:** DCM create request with `spec.ports`, `spec.metadata.name`, and
  `spec.service_type: network`
  ```json
  {
    "spec": {
      "service_type": "network",
      "ports": [{"name": "http", "protocol": "TCP", "port": 80, "target_port": 8080}],
      "metadata": {"name": "web-frontend"}
    }
  }
  ```
- **When:** Service spec is built
- **Then:**
  - Service `spec.ports[0]` = `{name: http, protocol: TCP, port: 80, targetPort: 8080}`
  - Service `metadata.name = "web-frontend"`
  - Service `spec.type = "ClusterIP"` (default when routing_level and node_ports omitted)

### TC-U011: Apply selector from provider_hints

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request with `provider_hints.kubernetes.selector = {"app": "web"}`
- **When:** Service spec is built
- **Then:** Service `spec.selector = {"app": "web"}`

### TC-U012: Apply cluster_ip from provider_hints

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request with `provider_hints.kubernetes.cluster_ip = "None"`
- **When:** Service spec is built
- **Then:** Service `spec.clusterIP = "None"` (headless)

### TC-U013: Apply node_ports from provider_hints

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request with `provider_hints.kubernetes.node_ports = {"http": 30080}`
- **When:** Service spec is built
- **Then:** Service port named `http` has `nodePort: 30080`

### TC-U014: Apply all provider_hints (selector, cluster_ip, node_ports)

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request with `provider_hints.kubernetes.selector`,
  `cluster_ip`, and `node_ports` all set
- **When:** Service spec is built
- **Then:** All three settings applied to Service spec

### TC-U015: Generate DCM labels on Service

- **Priority:** High
- **Type:** Unit
- **Given:** DCM instance ID `"abc-123"`
- **When:** Service spec is built
- **Then:** Service has labels:
  - `dcm.project/managed-by: "dcm"`
  - `dcm.project/dcm-instance-id: "abc-123"`
  - `dcm.project/dcm-service-type: "network"`

### TC-U016: Service created in configured namespace

- **Priority:** High
- **Type:** Unit
- **Given:** SP configured with `SP_K8S_NAMESPACE=team-a`
- **When:** Service spec is built
- **Then:** Service `metadata.namespace = "team-a"`

---

## 3 · Request Validation

> **Phase:** Network CRUD follow-up
> **Suggested Ginkgo structure:** `Describe("Request Validation")`

### TC-U020: Validate ports is required

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request without `ports` field
- **When:** Request is validated
- **Then:** Validation error returned: "ports is required"

### TC-U021: Validate port fields

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request with invalid port values:
  - Missing `port` (required)
  - Missing `name` when multiple ports specified
  - Invalid `protocol` value
- **When:** Request is validated
- **Then:** Validation error returned for each case

### TC-U022: Accept valid port configurations

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request with valid port configurations:
  - Single port: `{"name": "http", "protocol": "TCP", "port": 80, "target_port": 8080}`
  - Multiple ports: `[{"name": "http", ...}, {"name": "grpc", ...}]`
- **When:** Request is validated
- **Then:** All configurations pass validation

### TC-U023: Validate metadata.name is required

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request without `metadata.name`
- **When:** Request is validated
- **Then:** Validation error returned: "metadata.name is required"

### TC-U024: Validate metadata.name format (DNS-1123)

- **Priority:** Medium
- **Type:** Unit
- **Given:** DCM request with invalid names:
  - `"Test-Service"` (uppercase)
  - `"test_service"` (underscore)
  - `"test.service"` (dot)
  - String exceeding 63 characters
- **When:** Request is validated
- **Then:** Validation error returned for each case

### TC-U025: Validate routing_level enum values

- **Priority:** Medium
- **Type:** Unit
- **Given:** DCM request with `routing_level: "application"`
- **When:** Request is validated
- **Then:** Validation error: routing_level `application` is not supported in v1

### TC-U026: Validate service_type must be "network"

- **Priority:** Medium
- **Type:** Unit
- **Given:** DCM request with `service_type: "storage"`
- **When:** Request is validated
- **Then:** Validation error: service_type must be `network`

---

## 4 · Status Mapping

> **Phase:** Network CRUD follow-up
> **Suggested Ginkgo structure:** `Describe("Status Mapping")`

### TC-U030: Map LoadBalancer with empty ingress to PENDING

- **Priority:** High
- **Type:** Unit
- **Given:** Service with `spec.type = "LoadBalancer"` and
  `status.loadBalancer.ingress[]` is empty
- **When:** Status is mapped to DCM status
- **Then:** DCM status = `"PENDING"`

### TC-U031: Map LoadBalancer with ingress entries to READY

- **Priority:** High
- **Type:** Unit
- **Given:** Service with `spec.type = "LoadBalancer"` and
  `status.loadBalancer.ingress[]` has entries
- **When:** Status is mapped to DCM status
- **Then:** DCM status = `"READY"`

### TC-U032: Map ClusterIP Service to READY

- **Priority:** High
- **Type:** Unit
- **Given:** Service with `spec.type = "ClusterIP"`
- **When:** Status is mapped to DCM status
- **Then:** DCM status = `"READY"`

### TC-U033: Map NodePort Service to READY

- **Priority:** High
- **Type:** Unit
- **Given:** Service with `spec.type = "NodePort"`
- **When:** Status is mapped to DCM status
- **Then:** DCM status = `"READY"`

### TC-U034: Map Service not found to DELETED

- **Priority:** High
- **Type:** Unit
- **Given:** Service does not exist in cluster
- **When:** Status is queried
- **Then:** DCM status = `"DELETED"`

### TC-U035: Map headless ClusterIP to READY

- **Priority:** Medium
- **Type:** Unit
- **Given:** Service with `spec.type = "ClusterIP"` and
  `spec.clusterIP = "None"` (headless)
- **When:** Status is mapped to DCM status
- **Then:** DCM status = `"READY"`

---

## 5 · CloudEvent Construction

> **Phase:** Monitoring follow-up
> **Suggested Ginkgo structure:** `Describe("CloudEvent Construction")`

### TC-U040: Build CloudEvent with correct attributes

- **Priority:** High
- **Type:** Unit
- **Given:** Service status change (PENDING -> READY)
- **When:** CloudEvent is constructed
- **Then:** CloudEvent has:
  - `id`: non-empty unique identifier
  - `source`: `"dcm/providers/{provider-name}"`
  - `type`: `"dcm.status.network"`
  - `subject`: `"dcm.network"`
  - `datacontenttype`: `"application/json"`
  - `specversion`: `"1.0"`

### TC-U041: Build CloudEvent data payload

- **Priority:** High
- **Type:** Unit
- **Given:** Service with `dcm-instance-id = "abc-123"` and status `"READY"`
- **When:** CloudEvent is constructed
- **Then:** CloudEvent data contains:
  ```json
  {
    "id": "abc-123",
    "status": "READY",
    "message": "Service is ready"
  }
  ```

### TC-U042: CloudEvent includes service details when ready

- **Priority:** Medium
- **Type:** Unit
- **Given:** Ready Service with `spec.clusterIP = "10.96.0.1"`
- **When:** CloudEvent is constructed
- **Then:** CloudEvent data includes service state information

### TC-U043: CloudEvent source uses configured provider name

- **Priority:** High
- **Type:** Unit
- **Given:** SP configured with `SP_NAME=k8s-network-sp`
- **When:** CloudEvent is constructed for any status change
- **Then:** CloudEvent `source` = `"dcm/providers/k8s-network-sp"`

### TC-U044: CloudEvent specversion and datacontenttype are set

- **Priority:** Medium
- **Type:** Unit
- **Given:** Any status change event
- **When:** CloudEvent is constructed
- **Then:**
  - `specversion` = `"1.0"`
  - `datacontenttype` = `"application/json"`

---

## 6 · Network Update Validation (Out of v1 Scope)

Day-2 `UPDATE` is out of v1 scope. No `PATCH` handler tests apply to v1.

---

## 7 · API Handlers (with Mocked K8s Client)

> **Phase:** Network CRUD follow-up
> **Suggested Ginkgo structure:** `Describe("API Handlers")` with nested contexts

### TC-U060: POST /networks creates Service via mocked client

- **Priority:** High
- **Type:** Unit
- **Given:**
  - Mocked K8s client
  - Valid DCM request with ports and metadata.name
- **When:** POST /networks handler is called
- **Then:**
  - Mocked K8s client `Create(Service)` called once
  - Response status: 201 Created
  - Response body contains network details

### TC-U061: POST /networks returns 409 if Service name exists

- **Priority:** High
- **Type:** Unit
- **Given:**
  - Mocked K8s client configured to return "already exists" error
- **When:** POST /networks handler is called
- **Then:**
  - Response status: 409 Conflict
  - Error message indicates Service already exists

### TC-U062: POST /networks returns 400 for invalid request

- **Priority:** High
- **Type:** Unit
- **Given:** DCM request missing required field (ports)
- **When:** POST /networks handler is called
- **Then:**
  - Response status: 400 Bad Request
  - Error message indicates missing ports

### TC-U063: GET /networks lists Services from mocked client

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client returns 3 Services with DCM labels
- **When:** GET /networks handler is called
- **Then:**
  - Response status: 200 OK
  - Response contains 3 networks

### TC-U064: GET /networks filters by DCM labels

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client returns mixed Services (some with DCM labels, some
  without)
- **When:** GET /networks handler is called
- **Then:** Response contains only Services with
  `dcm.project/managed-by=dcm` and `dcm.project/dcm-service-type=network`

### TC-U065: GET /networks/{id} returns single Service

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client returns Service with `dcm-instance-id = "abc-123"`
- **When:** GET /networks/abc-123 handler is called
- **Then:**
  - Response status: 200 OK
  - Response contains network details

### TC-U066: GET /networks/{id} returns 404 if not found

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client returns "not found" error
- **When:** GET /networks/nonexistent handler is called
- **Then:**
  - Response status: 404 Not Found

### TC-U069: DELETE /networks/{id} deletes Service

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client with existing Service
- **When:** DELETE /networks/{id} handler is called
- **Then:**
  - Mocked K8s client `Delete(Service)` called once
  - Response status: 204 No Content

### TC-U070: DELETE /networks/{id} returns 404 if not found

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client returns "not found" error
- **When:** DELETE /networks/nonexistent handler is called
- **Then:**
  - Response status: 404 Not Found

### TC-U071: POST /networks without ?id= returns server-generated ID

- **Priority:** High
- **Type:** Unit
- **Given:** Valid DCM request, no `?id=` query parameter
- **When:** POST /networks handler is called
- **Then:**
  - Response status: 201 Created
  - Response `id` is a non-empty string conforming to AEP-122 pattern
    `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`

### TC-U072: POST /networks with ?id= uses client-specified ID

- **Priority:** High
- **Type:** Unit
- **Given:** Valid DCM request with query parameter `?id=web-frontend-svc`
- **When:** POST /networks handler is called
- **Then:**
  - Response status: 201 Created
  - Response `id` = `"web-frontend-svc"`

### TC-U073: POST /networks with invalid ?id= returns 400

- **Priority:** Medium
- **Type:** Unit
- **Given:** DCM request with invalid `?id=` values:
  - `"Web-Frontend"` (uppercase)
  - String exceeding 63 characters
  - `"-starts-with-dash"`
- **When:** POST /networks handler is called
- **Then:**
  - Response status: 400 Bad Request
  - Error indicates invalid ID format

### TC-U074: POST /networks response populates all read-only fields

- **Priority:** High
- **Type:** Unit
- **Given:** Valid DCM request creating a ClusterIP Service
- **When:** POST /networks handler is called
- **Then:** Response MUST include:
  - `id`: server-generated or client-specified
  - `path`: `"networks/{network_id}"`
  - `status`: `"READY"` (ClusterIP)
  - `metadata.namespace`: configured namespace
  - `kubernetes.type`: `"ClusterIP"`
  - `kubernetes.cluster_ip`: assigned cluster IP

### TC-U075: POST /networks with conflicting NodePort returns 409

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client configured to return "nodePort conflict" error
- **When:** POST /networks handler is called with `node_ports: {"http": 30080}`
- **Then:**
  - Response status: 409 Conflict
  - Error indicates NodePort conflict

### TC-U076: GET /networks with no networks returns 200 with empty results

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client returns empty list
- **When:** GET /networks handler is called
- **Then:**
  - Response status: 200 OK
  - Response body has `results: []` (empty array, not null)

### TC-U077: Error responses use RFC 7807 format

- **Priority:** High
- **Type:** Unit
- **Given:** Any error condition (400, 404, 409, 500)
- **When:** Error response is returned
- **Then:**
  - `Content-Type: application/problem+json`
  - Response body includes `type` and `title` fields at minimum
  - Error `type` maps to appropriate HTTP status per error mapping table

### TC-U078: GET/DELETE with multiple Services matching same instance ID returns conflict

- **Priority:** Medium
- **Type:** Unit
- **Given:** Mocked K8s client returns 2 Services both having
  `dcm.project/dcm-instance-id = "abc-123"`
- **When:** GET /networks/abc-123 or DELETE /networks/abc-123 handler is called
- **Then:**
  - Response status: 409 Conflict
  - Error indicates ambiguous instance ID

---

## 8 · Health Endpoint

> **Suggested Ginkgo structure:** `Describe("Health Endpoint")`

### TC-U080: GET /api/v1alpha1/networks/health returns healthy status

- **Priority:** High
- **Type:** Unit
- **Given:** SP is running with mocked K8s client (healthy)
- **When:** GET `/api/v1alpha1/networks/health` handler is called
- **Then:**
  - Response status: 200 OK
  - Response body:
    ```json
    {
      "status": "healthy",
      "type": "k8s-network-service-provider.dcm.io/health",
      "path": "health",
      "version": "<version>",
      "uptime": "<seconds>"
    }
    ```

### TC-U081: GET /api/v1alpha1/networks/health returns unhealthy when K8s unreachable

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client health check returns error
- **When:** GET `/api/v1alpha1/networks/health` handler is called
- **Then:**
  - Response status: 200 OK (per DCM convention)
  - Response body:
    - `status`: `"unhealthy"`
    - `type`: `"k8s-network-service-provider.dcm.io/health"`
    - `path`: `"health"`
    - `version`: SP build version (string)
    - `uptime`: seconds since SP started (integer)

### TC-U082: CheckHealth succeeds when K8s API is reachable

- **Priority:** High
- **Type:** Unit
- **Given:** `Discovery().ServerVersion()` returns without error
- **When:** `CheckHealth()` is called
- **Then:** No error is returned

### TC-U083: CheckHealth returns error when K8s API is unreachable

- **Priority:** High
- **Type:** Unit
- **Given:** `Discovery().ServerVersion()` returns error (simulated failure)
- **When:** `CheckHealth()` is called
- **Then:** Error is returned indicating K8s API is unreachable

---

## 9 · Error Handling

> **Suggested Ginkgo structure:** `Describe("Error Handling")`

### TC-U090: Handle K8s API errors gracefully

- **Priority:** High
- **Type:** Unit
- **Given:** Mocked K8s client returns various errors (timeout, unauthorized,
  internal)
- **When:** Any handler is called
- **Then:** Appropriate HTTP error code and RFC 7807 response returned

### TC-U091: Handle invalid JSON in request body

- **Priority:** Medium
- **Type:** Unit
- **Given:** POST request with malformed JSON
- **When:** Handler is called
- **Then:**
  - Response status: 400 Bad Request
  - Error message indicates JSON parsing error

### TC-U092: Handle missing required headers

- **Priority:** Low
- **Type:** Unit
- **Given:** Request without Content-Type header
- **When:** POST handler is called
- **Then:** Request handled or appropriate error returned

---

## 10 · Status Monitoring

> **Phase:** Monitoring follow-up
> **Suggested Ginkgo structure:** `Describe("Status Monitoring")`

### TC-U100: Debounce suppresses duplicate events within interval

- **Priority:** High
- **Type:** Unit
- **Given:** Multiple status changes occur within the debounce interval for the
  same instance ID
- **When:** Events are processed
- **Then:** Only the last status within the debounce window is published

### TC-U101: Per-instance debounce isolation

- **Priority:** High
- **Type:** Unit
- **Given:** Status changes occur within the debounce interval for two different
  instance IDs
- **When:** Events are processed
- **Then:** Each instance's events are debounced independently

### TC-U102: Instance ID extracted from label

- **Priority:** High
- **Type:** Unit
- **Given:** Service event with label `dcm.project/dcm-instance-id = "abc-123"`
- **When:** The event handler processes it
- **Then:** The instance ID used for status publishing is `"abc-123"`

### TC-U103: Informer started after HTTP server ready

- **Priority:** High
- **Type:** Unit
- **Given:** SP startup sequence
- **When:** The monitoring subsystem initializes
- **Then:** The informer is started as an asynchronous background task after the
  HTTP server has begun listening

### TC-U104: Informer stopped on shutdown

- **Priority:** High
- **Type:** Unit
- **Given:** SP receives a shutdown signal
- **When:** Graceful shutdown begins
- **Then:** The informer is stopped before the process exits

### TC-U105: Resync re-evaluates all tracked Services

- **Priority:** Medium
- **Type:** Unit
- **Given:** The informer is running and the resync period elapses
- **When:** Resync fires
- **Then:** Status reconciliation is re-evaluated for every Service in the cache

### TC-U106: Initial sync publishes events for all existing Services

- **Priority:** High
- **Type:** Unit
- **Given:** The SP starts with existing DCM-managed Services in the cluster
- **When:** The informer cache completes initial synchronization
- **Then:** A status CloudEvent is published for each existing Service

### TC-U107: StatusPublisher interface decouples transport

- **Priority:** Medium
- **Type:** Unit
- **Given:** The status publishing subsystem
- **When:** Status events are published
- **Then:** Publishing is decoupled from the NATS transport via a
  `StatusPublisher` interface (mockable for tests)

---

## 11 · Registration

> **Suggested Ginkgo structure:** `Describe("Registration")`

### TC-U110: BuildPayload sets correct network registration fields

- **Priority:** High
- **Type:** Unit
- **Given:** Provider configuration is set
- **When:** `BuildPayload` constructs the registration request
- **Then:** Payload has:
  - `service_type = "network"`
  - `endpoint` ending in `/api/v1alpha1/networks`
  - `operations = ["CREATE", "READ", "DELETE"]`
  - `schema_version = "v1alpha1"`

### TC-U111: Sends POST to /providers on startup

- **Priority:** High
- **Type:** Unit
- **Given:**
  - `httptest.Server` mocking DCM registration endpoint
  - SP configured with mock DCM URL
- **When:** Registration starts
- **Then:** POST request sent to `/providers`

### TC-U112: Payload includes all registration fields

- **Priority:** High
- **Type:** Unit
- **Given:** `httptest.Server` accepting registration
- **When:** Registration request is sent
- **Then:** Payload includes `name`, `service_type`, `endpoint`, `operations`,
  `schema_version`, `display_name`, `metadata` (region, zone)

### TC-U113: Start() returns within 1s; registration completes in background

- **Priority:** High
- **Type:** Unit
- **Given:** `httptest.Server` with a delayed response
- **When:** `Start()` is called
- **Then:** `Start()` returns immediately; registration completes asynchronously

### TC-U114: Retries with increasing intervals; succeeds on 4th attempt

- **Priority:** High
- **Type:** Unit
- **Given:** `httptest.Server` returns 500 for first 3 attempts, 200 on 4th
- **When:** Registration runs
- **Then:** Registrar retries with exponential backoff and succeeds on attempt 4

### TC-U115: Logs WARN on 5xx; keeps retrying without exiting

- **Priority:** Medium
- **Type:** Unit
- **Given:** `httptest.Server` returns 500 continuously
- **When:** Registration retries
- **Then:** WARN-level logs are emitted; SP does not exit

### TC-U116: Stops retrying on 4xx; logs ERROR with "non-retryable"

- **Priority:** High
- **Type:** Unit
- **Given:** `httptest.Server` returns 400
- **When:** Registration attempt receives this response
- **Then:** No further retries; ERROR log with "non-retryable" emitted

### TC-U117: Multiple Start() calls launch only one goroutine

- **Priority:** Medium
- **Type:** Unit
- **Given:** Registrar instance
- **When:** `Start()` is called multiple times
- **Then:** Only one registration goroutine runs

### TC-U118: Done() channel closes after successful registration

- **Priority:** Medium
- **Type:** Unit
- **Given:** `httptest.Server` returns 200
- **When:** Registration succeeds
- **Then:** `Done()` channel is closed

---

## 12 · Utilities

> **Suggested Ginkgo structure:** `Describe("Utilities")`

### TC-U120: PostPath returns correct network path

- **Priority:** Medium
- **Type:** Unit
- **Given:** API path constants are defined
- **When:** `PostPath()` is called
- **Then:** Returns `/api/v1alpha1/networks`

### TC-U121: Ptr returns pointer to string

- **Priority:** Low
- **Type:** Unit
- **Given:** String value `"hello"`
- **When:** `Ptr("hello")` is called
- **Then:** Returns pointer to `"hello"`

### TC-U122: Ptr returns pointer to integer

- **Priority:** Low
- **Type:** Unit
- **Given:** Integer value `42`
- **When:** `Ptr(42)` is called
- **Then:** Returns pointer to `42`

### TC-U123: Ptr returns pointer to boolean

- **Priority:** Low
- **Type:** Unit
- **Given:** Boolean value `true`
- **When:** `Ptr(true)` is called
- **Then:** Returns pointer to `true`

---

## Test Execution

### Running Unit Tests

```bash
cd k8s-network-service-provider
make test-unit
```

Or directly with Ginkgo:

```bash
ginkgo -r --skip-package=test/integration
```

### Coverage Target

- **Minimum:** 80% code coverage
- **Target:** 90% code coverage
- **Focus areas:** Request validation, status mapping, service spec building

### Mocking Libraries

- **Kubernetes client:** `k8s.io/client-go/kubernetes/fake`
- **NATS publisher:** Custom interface mock
- **HTTP testing:** `net/http/httptest`

---

## Coverage Matrix

| Spec Requirement | Test Case IDs |
|------------------|---------------|
| REQ-HTTP-020 | TC-U120 |
| REQ-HTTP-050 | TC-U001, TC-U002, TC-U003 |
| REQ-HTTP-070 | TC-U090 |
| REQ-HTTP-090 | TC-U091 |
| REQ-HLT-010 | TC-U080 |
| REQ-HLT-020 | TC-U080, TC-U081 |
| REQ-HLT-050 | TC-U082, TC-U083 |
| REQ-HLT-060 | TC-U081 |
| REQ-API-020 | TC-U060 |
| REQ-API-030 | TC-U071 |
| REQ-API-040 | TC-U072 |
| REQ-API-041 | TC-U073 |
| REQ-API-050 | TC-U026 |
| REQ-API-060 | TC-U020, TC-U023, TC-U062 |
| REQ-API-070 | TC-U024 |
| REQ-API-080 | TC-U010, TC-U074 |
| REQ-API-090 | TC-U074 |
| REQ-API-100 | TC-U061 |
| REQ-API-105 | TC-U075 |
| REQ-API-110 | TC-U025 |
| REQ-API-120 | TC-U063 |
| REQ-API-121 | TC-U076 |
| REQ-API-140 | TC-U065 |
| REQ-API-150 | TC-U066 |
| REQ-API-210 | TC-U069 |
| REQ-API-220 | TC-U070 |
| REQ-API-230 | TC-U077 |
| REQ-API-231 | TC-U077 |
| REQ-API-240 | TC-U011, TC-U012, TC-U013, TC-U014 |
| REQ-STR-020 | TC-U060 |
| REQ-STR-030 | TC-U061 |
| REQ-STR-040 | TC-U065 |
| REQ-STR-050 | TC-U063 |
| REQ-STR-080 | TC-U069 |
| REQ-STR-090 | TC-U066, TC-U061 |
| REQ-STR-100 | TC-U083 |
| REQ-K8S-020 | TC-U016 |
| REQ-K8S-040 | TC-U015 |
| REQ-K8S-060 | TC-U064 |
| REQ-K8S-070 | TC-U010 |
| REQ-K8S-080 | TC-U010 |
| REQ-K8S-090 | TC-U011 |
| REQ-K8S-100 | TC-U012 |
| REQ-K8S-110 | TC-U013 |
| REQ-K8S-150 | TC-U069 |
| REQ-K8S-170 | TC-U030, TC-U031, TC-U032, TC-U033, TC-U034, TC-U035 |
| REQ-K8S-190 | TC-U078 |
| REQ-MON-070 | TC-U040 |
| REQ-MON-080 | TC-U040 |
| REQ-MON-090 | TC-U043 |
| REQ-MON-095 | TC-U040 |
| REQ-MON-100 | TC-U044 |
| REQ-MON-110 | TC-U041 |
| REQ-MON-120 | TC-U030, TC-U031, TC-U032, TC-U033, TC-U034 |
| REQ-MON-130 | TC-U100 |
| REQ-MON-140 | TC-U101 |
| REQ-MON-150 | TC-U102 |
| REQ-MON-160 | TC-U103 |
| REQ-MON-161 | TC-U104 |
| REQ-MON-170 | TC-U105 |
| REQ-MON-175 | TC-U106 |
| REQ-MON-200 | TC-U107 |
| REQ-REG-010 | TC-U111 |
| REQ-REG-020 | TC-U110, TC-U112 |
| REQ-REG-030 | TC-U113 |
| REQ-REG-031 | TC-U113 |
| REQ-REG-040 | TC-U110 |
| REQ-REG-041 | TC-U110 |
| REQ-REG-042 | TC-U110 |
| REQ-REG-043 | TC-U110 |
| REQ-REG-050 | TC-U114, TC-U116 |
| REQ-REG-060 | TC-U115, TC-U116 |
| REQ-REG-061 | TC-U115 |
| REQ-XC-CFG-010 | TC-U001 |
| REQ-XC-CFG-020 | TC-U003 |
| REQ-XC-ERR-010 | TC-U077 |
| REQ-XC-ERR-020 | TC-U077 |
| REQ-XC-ID-010 | TC-U071, TC-U072 |
| REQ-XC-ID-020 | TC-U061 |
| REQ-XC-LBL-010 | TC-U015 |

---

## Notes

- All unit tests should execute in < 1 second total
- No network calls to real services
- No file I/O except for config loading tests
- Tests should be deterministic and repeatable
- Use table-driven tests where applicable (e.g., port validation, status mapping)
