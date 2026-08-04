# Test Plan: K8s Network SP — Integration Tests

## Overview

- **Related Spec:** .ai/specs/k8s-network-sp.spec.md
- **Framework:** Ginkgo v2 + Gomega
- **Created:** 2026-07-28
- **Last Updated:** 2026-07-30

Integration tests verify components working together with **real external services**.
These tests require a real Kubernetes cluster (Kind), and (for full suite) a real
NATS server.

**Implementation phases:**

| Phase | Scope | Sections |
|-------|-------|----------|
| Health + registration (current) | §1–§2 | Server lifecycle, DCM registration |
| Network CRUD (follow-up) | §3–§6, §9 | Service create/read/delete, type inference |
| Monitoring (follow-up) | §7–§8 | NATS CloudEvents, informer |
| Error scenarios (mixed) | §10 | NATS failure, K8s unavailable, kubeconfig |

Until network handlers and monitoring land, §3–§10 expect **500** on network
routes or are not runnable.

**Real services required:**
- Kind Kubernetes cluster (or similar test cluster)
- NATS server (for CloudEvents)
- SP running as real process

**What integration tests verify:**
- SP lifecycle (startup, registration, shutdown)
- Real Service operations against Kubernetes
- Status reporting to real NATS
- Informer watches real Service changes
- Service type inference scenarios

---

## Test Environment Setup

### Required Infrastructure

1. **Kind Cluster**
   ```bash
   kind create cluster --name sp-test
   ```

2. **NATS Server**
   ```bash
   docker run -d -p 4222:4222 -p 8222:8222 nats:2-alpine --jetstream
   ```

3. **SP Configuration (env vars)**
   ```bash
   SP_K8S_NAMESPACE=dcm-test
   SP_NATS_URL=nats://localhost:4222
   DCM_REGISTRATION_URL=http://localhost:8080/api/v1alpha1
   SP_NAME=k8s-network-sp
   SP_ENDPOINT=http://localhost:8080
   ```

### Test Data Fixtures

- Sample Service manifests
- Sample DCM requests (JSON payloads)

---

## 1 · Server Lifecycle

> **Suggested Ginkgo structure:** `Describe("Server Lifecycle")`

### TC-I001: SP starts up successfully

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running and SP is not started
- **When:** SP process starts with valid configuration
- **Then:**
  - Process starts without errors
  - Logs indicate successful startup
  - Health endpoint becomes reachable

### TC-I002: All API endpoints are accessible

- **Priority:** High
- **Type:** Integration
- **Given:** SP is running
- **When:** HTTP requests are made to each endpoint
- **Then:**
  - `GET /api/v1alpha1/networks/health` returns 200
  - `POST /api/v1alpha1/networks` does not return 404/405
  - `GET /api/v1alpha1/networks` does not return 404/405
  - `GET /api/v1alpha1/networks/{network_id}` does not return 404/405 (may return
    500 until implemented)
  - `DELETE /api/v1alpha1/networks/{network_id}` does not return 404/405 (may
    return 500 until implemented)

### TC-I003: SP shuts down gracefully on SIGTERM

- **Priority:** High
- **Type:** Integration
- **Given:** SP is running with active informer
- **When:** SIGTERM is sent to process
- **Then:**
  - Informer stops gracefully
  - In-flight requests complete
  - Process exits with code 0

### TC-I004: SP logs startup and shutdown events

- **Priority:** Medium
- **Type:** Integration
- **Given:** SP lifecycle (start -> stop)
- **When:** Observing logs
- **Then:**
  - Startup log includes listen address
  - Shutdown log indicates graceful termination

### TC-I005: SP shuts down gracefully on SIGINT

- **Priority:** High
- **Type:** Integration
- **Given:** SP is running with active informer
- **When:** SIGINT is sent to process
- **Then:**
  - Behavior identical to SIGTERM (TC-I003)
  - Informer stops gracefully
  - In-flight requests complete
  - Process exits with code 0

---

## 2 · SP Registration

> **Suggested Ginkgo structure:** `Describe("SP Registration")`
> **Automated coverage:** `internal/registration/registration_integration_test.go` uses
> `httptest` to exercise `Registrar.Start()` against a mock DCM registry (POST
> `/providers`, retry on 5xx, no retry on 4xx, non-blocking `Start()`). Maps to
> AC-REG-010–050. Full E2E with real control-plane remains manual (TC-I010–I011).

### TC-I010: SP registers with DCM on startup

- **Priority:** High
- **Type:** Integration
- **Given:**
  - DCM control-plane (or WireMock stub) is running
  - SP configured with DCM registrar URL
- **When:** SP starts
- **Then:**
  - POST request sent to DCM `POST /api/v1alpha1/providers`
  - Request body includes:
    - `name`: configured provider name
    - `endpoint`: `{SP_ENDPOINT}/api/v1alpha1/networks`
    - `service_type`: `"network"`
    - `schema_version`: `"v1alpha1"`
    - `operations`: `["CREATE", "READ", "DELETE"]`

### TC-I011: Health endpoint responds after registration

- **Priority:** High
- **Type:** Integration
- **Given:** SP has registered with DCM
- **When:** DCM polls `GET /api/v1alpha1/networks/health`
- **Then:**
  - Response: 200 OK
  - Body includes: `status`, `type`, `path`, `version`, `uptime`
    (e.g., `"status": "healthy"`)

---

## 3 · Service Creation (Real Kubernetes)

> **Phase:** Network CRUD follow-up (not in health+registration PR)
> **Suggested Ginkgo structure:** `Describe("Service Creation")`

### TC-I020: Create Service with minimal spec

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running, SP is running
- **When:** POST `/api/v1alpha1/networks`:
  ```json
  {
    "spec": {
      "ports": [{"name": "http", "protocol": "TCP", "port": 80, "target_port": 8080}],
      "metadata": {"name": "test-svc-minimal"},
      "service_type": "network"
    }
  }
  ```
- **Then:**
  - Response: 201 Created with network details
  - Service exists in configured namespace (`dcm-test`)
  - Service has correct labels:
    - `dcm.project/managed-by: dcm`
    - `dcm.project/dcm-instance-id: <aep-122-id>`
    - `dcm.project/dcm-service-type: network`
  - Service `spec.ports[0]` = `{name: http, protocol: TCP, port: 80, targetPort: 8080}`
  - Service `spec.type = "ClusterIP"` (default)

### TC-I021: Create Service with selector hint

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running
- **When:** POST with `provider_hints.kubernetes.selector: {"app": "web"}`
- **Then:**
  - Service created with `spec.selector: {"app": "web"}`

### TC-I022: Create Service with cluster_ip hint

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running
- **When:** POST with `provider_hints.kubernetes.cluster_ip: "None"`
- **Then:** Service created with `spec.clusterIP: "None"` (headless)

### TC-I023: Create Service with node_ports hint (NodePort type)

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running
- **When:** POST with `provider_hints.kubernetes.node_ports: {"http": 30080}`
  (routing_level omitted)
- **Then:**
  - Service created with `spec.type: "NodePort"`
  - Port named `http` has `nodePort: 30080`

### TC-I024: Create Service with routing_level: network (LoadBalancer type)

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running
- **When:** POST with `routing_level: "network"` and no `node_ports`
- **Then:**
  - Service created with `spec.type: "LoadBalancer"`
  - Status is `PENDING` (no external IP in Kind without MetalLB)

### TC-I025: Create duplicate Service name returns 409

- **Priority:** High
- **Type:** Integration
- **Given:** Service with name `"web-frontend"` already exists
- **When:** POST with same name `"web-frontend"`
- **Then:**
  - Response: 409 Conflict
  - Error message indicates Service already exists

### TC-I026: Create multiple Services in same namespace

- **Priority:** High
- **Type:** Integration
- **Given:** SP configured with namespace `dcm-test`
- **When:** Create 5 Services with different names
- **Then:**
  - All 5 Services created successfully
  - All exist in namespace `dcm-test`
  - All have correct DCM labels

### TC-I027: Create Service without ?id= returns server-generated ID

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running
- **When:** POST `/api/v1alpha1/networks` without `?id=` query parameter
- **Then:**
  - Response: 201 Created
  - Response `id` is a non-empty string conforming to AEP-122 pattern
  - Service has `dcm.project/dcm-instance-id` label set to the generated ID

### TC-I028: Create Service with ?id= uses client-specified ID

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running
- **When:** POST `/api/v1alpha1/networks?id=my-custom-svc`
- **Then:**
  - Response: 201 Created
  - Response `id` = `"my-custom-svc"`
  - Service has `dcm.project/dcm-instance-id = "my-custom-svc"` label

---

## 4 · Service Reading (Real Kubernetes)

> **Phase:** Network CRUD follow-up
> **Suggested Ginkgo structure:** `Describe("Service Reading")`

### TC-I030: GET single network returns Service details

- **Priority:** High
- **Type:** Integration
- **Given:** Service exists with `dcm-instance-id = "web-svc"`
- **When:** GET `/api/v1alpha1/networks/web-svc`
- **Then:**
  - Response: 200 OK
  - Response body includes:
    - `id`: `"web-svc"`
    - `path`: `"networks/web-svc"`
    - `spec.ports`: port configuration
    - `spec.metadata.name`: Service name
    - `status`: current status (`PENDING` or `READY`)
    - `kubernetes.type`: Service type
    - `kubernetes.cluster_ip`: assigned cluster IP

### TC-I031: GET network returns PENDING for LoadBalancer without ingress

- **Priority:** High
- **Type:** Integration
- **Given:** LoadBalancer Service exists with empty
  `status.loadBalancer.ingress[]` (e.g., Kind without MetalLB)
- **When:** GET network
- **Then:** Response status field is `"PENDING"`

### TC-I032: GET network returns READY for ClusterIP or NodePort

- **Priority:** High
- **Type:** Integration
- **Given:** ClusterIP or NodePort Service exists
- **When:** GET network
- **Then:**
  - Response status field is `"READY"`
  - `kubernetes.cluster_ip` is populated

### TC-I033: GET network returns 404 for non-existent

- **Priority:** High
- **Type:** Integration
- **Given:** No Service with `dcm-instance-id` matching the requested ID
- **When:** GET `/api/v1alpha1/networks/nonexistent-id`
- **Then:** Response: 404 Not Found

### TC-I034: LIST networks returns all managed Services

- **Priority:** High
- **Type:** Integration
- **Given:** 3 Services with DCM labels exist
- **When:** GET `/api/v1alpha1/networks`
- **Then:**
  - Response: 200 OK
  - Response contains 3 networks in `results` array

### TC-I035: LIST networks filters by DCM labels only

- **Priority:** High
- **Type:** Integration
- **Given:**
  - 2 Services with `dcm.project/managed-by=dcm` label
  - 1 Service without DCM labels (manually created)
- **When:** GET `/api/v1alpha1/networks`
- **Then:** Response contains only the 2 Services with DCM labels

### TC-I036: LIST networks with pagination

- **Priority:** Medium
- **Type:** Integration
- **Given:** 10 Services exist
- **When:** GET `/api/v1alpha1/networks?max_page_size=5`
- **Then:**
  - Response contains 5 networks
  - `next_page_token` is present
  - Second request with page_token returns next 5

### TC-I037: LIST networks with no managed Services returns empty results

- **Priority:** High
- **Type:** Integration
- **Given:** No DCM-managed Services exist in the namespace
- **When:** GET `/api/v1alpha1/networks`
- **Then:**
  - Response: 200 OK
  - Response body has `results: []` (empty array)

---

## 5 · Network Update (Out of v1 Scope)

Day-2 `UPDATE` is a **non-goal** for v1. No `PATCH` endpoint is defined in
`api/v1alpha1/openapi.yaml`. Update test cases are excluded from v1 (post-v1
enhancement scope).

---

## 6 · Service Deletion (Real Kubernetes)

> **Phase:** Network CRUD follow-up
> **Suggested Ginkgo structure:** `Describe("Service Deletion")`

### TC-I050: Delete Service

- **Priority:** High
- **Type:** Integration
- **Given:** Service exists in the cluster
- **When:** DELETE `/api/v1alpha1/networks/{network_id}`
- **Then:**
  - Response: 204 No Content
  - Service is removed from Kubernetes cluster
  - GET on same ID returns 404

### TC-I051: Delete non-existent network returns 404

- **Priority:** High
- **Type:** Integration
- **Given:** No Service with given ID
- **When:** DELETE network
- **Then:** Response: 404 Not Found

### TC-I052: Delete and verify removal

- **Priority:** High
- **Type:** Integration
- **Given:** Service exists
- **When:**
  - DELETE network
  - Wait for deletion to complete
  - GET network
- **Then:** GET returns 404 Not Found

---

## 7 · Status Reporting (Real NATS)

> **Phase:** Monitoring follow-up
> **Suggested Ginkgo structure:** `Describe("Status Reporting")`

### TC-I060: CloudEvent published on Service creation

- **Priority:** High
- **Type:** Integration
- **Given:**
  - NATS server running
  - NATS subscriber listening on `dcm.network`
  - SP connected to NATS
- **When:** Create Service (ClusterIP)
- **Then:**
  - CloudEvent published to `dcm.network`
  - Event has `type: "dcm.status.network"`
  - Event data: `{"id": "<network-id>", "status": "READY", "message": "..."}`

### TC-I061: CloudEvent published on LoadBalancer ingress assignment

- **Priority:** High
- **Type:** Integration
- **Given:**
  - LoadBalancer Service exists with `status: PENDING`
  - Informer is watching
- **When:** External IP is assigned (`status.loadBalancer.ingress[]` populated)
- **Then:**
  - CloudEvent published with `status: "READY"`
  - Event data includes status message describing the transition

### TC-I062: CloudEvent published on Service deletion

- **Priority:** High
- **Type:** Integration
- **Given:** Service exists
- **When:** DELETE Service
- **Then:**
  - CloudEvent with `status: "DELETED"` published

### TC-I063: CloudEvent format validation

- **Priority:** High
- **Type:** Integration
- **Given:** Any Service status change
- **When:** CloudEvent is published
- **Then:** Event has required fields:
  - `id` (non-empty unique event identifier)
  - `source: "dcm/providers/{provider-name}"`
  - `type: "dcm.status.network"`
  - `subject: "dcm.network"`
  - `datacontenttype: "application/json"`
  - `specversion: "1.0"`
  - `data: {"id": "...", "status": "...", "message": "..."}`

### TC-I064: Multiple status updates published in sequence

- **Priority:** Medium
- **Type:** Integration
- **Given:** NATS subscriber tracking all events
- **When:** Create LoadBalancer Service -> wait for ingress -> delete Service
- **Then:** Receive events in order:
  1. PENDING (LoadBalancer created, no ingress)
  2. READY (ingress assigned)
  3. DELETED

### TC-I065: Debounce prevents duplicate events for rapid status changes

- **Priority:** High
- **Type:** Integration
- **Given:** NATS subscriber tracking all events for a specific instance
- **When:** Multiple rapid status oscillations occur within the debounce interval
- **Then:** Only the final status within each debounce window is published to NATS

### TC-I066: Status publish retries on transient NATS failure

- **Priority:** High
- **Type:** Integration
- **Given:**
  - NATS server is temporarily unreachable
  - A status change occurs
- **When:** NATS becomes available again
- **Then:**
  - The publisher retries with exponential backoff
  - The event is eventually published

---

## 8 · Informer Behavior

> **Phase:** Monitoring follow-up
> **Suggested Ginkgo structure:** `Describe("Informer Behavior")`

### TC-I070: Informer starts and watches Services

- **Priority:** High
- **Type:** Integration
- **Given:** SP starts with informer configured
- **When:** Informer starts
- **Then:**
  - Informer lists existing Services with DCM labels
  - Informer watches for changes

### TC-I071: Informer detects new Service

- **Priority:** High
- **Type:** Integration
- **Given:** Informer is running
- **When:** New Service with DCM labels is created
- **Then:** Informer Add event fires, status published to NATS

### TC-I072: Informer detects Service update

- **Priority:** High
- **Type:** Integration
- **Given:** Service exists and informer is watching
- **When:** Service status changes (e.g., LoadBalancer ingress assigned)
- **Then:** Informer Update event fires, new status published

### TC-I073: Informer detects Service deletion

- **Priority:** High
- **Type:** Integration
- **Given:** Service exists
- **When:** Service is deleted
- **Then:** Informer Delete event fires, DELETED status published

### TC-I074: Informer ignores non-DCM Services

- **Priority:** High
- **Type:** Integration
- **Given:** Informer is running
- **When:** Service without `dcm.project/managed-by=dcm` label is created
- **Then:** Informer does not publish event for this Service

### TC-I075: Informer auto-reconnects after API server disconnection

- **Priority:** High
- **Type:** Integration
- **Given:** Informer is running and the K8s API server connection is interrupted
- **When:** The API server becomes available again
- **Then:**
  - The informer automatically reconnects
  - Event processing resumes without manual intervention

### TC-I076: Initial cache sync publishes status for all existing Services

- **Priority:** High
- **Type:** Integration
- **Given:** DCM-managed Services already exist when the SP starts
- **When:** The informer cache completes initial synchronization
- **Then:** A status CloudEvent is published for each existing Service

---

## 9 · Service Type Inference Scenarios

> **Phase:** Network CRUD follow-up
> **Suggested Ginkgo structure:** `Describe("Service Type Inference Scenarios")`

### TC-I080: Create services with different type inferences

- **Priority:** High
- **Type:** Integration
- **Given:** Kind cluster is running
- **When:** Create Services with different configurations:
  - No `routing_level`, no `node_ports` -> ClusterIP
  - No `routing_level`, `node_ports` present -> NodePort
  - `routing_level: "network"`, no `node_ports` -> LoadBalancer
  - `routing_level: "network"`, `node_ports` present -> LoadBalancer with specified ports
- **Then:** Each Service uses the inferred type per the inference table

### TC-I081: Network Update (Out of v1 Scope)

Day-2 `UPDATE` is out of v1 scope. This scenario applies when network updates
are added in a post-v1 enhancement.

### TC-I082: Verify default service type when routing_level omitted

- **Priority:** Medium
- **Type:** Integration
- **Given:** No `routing_level` specified and no `node_ports`
- **When:** Create Service without type hints
- **Then:** Service is created as ClusterIP (Kubernetes default)

---

## 10 · Error Scenarios

> **Phase:** Mixed (TC-I091 health now; TC-I090 NATS when monitoring lands)
> **Suggested Ginkgo structure:** `Describe("Error Scenarios")`

### TC-I090: Handle NATS connection failure gracefully

- **Priority:** High
- **Type:** Integration
- **Given:** NATS server is stopped
- **When:** Service status changes
- **Then:**
  - SP logs error about NATS connection
  - SP continues operating (CRUD operations still work)
  - Status updates queue or are dropped (no crash)

### TC-I091: Handle Kubernetes API unavailable

- **Priority:** High
- **Type:** Integration
- **Given:** Kubernetes API becomes unreachable
- **When:** API request is made
- **Then:**
  - Appropriate error response (503 Service Unavailable or 500)
  - SP logs connection error
  - Health endpoint reports unhealthy

### TC-I092: Handle invalid kubeconfig

- **Priority:** High
- **Type:** Integration
- **Given:** SP configured with invalid kubeconfig path
- **When:** SP starts
- **Then:**
  - SP fails to start with clear error message
  - Error log indicates kubeconfig problem

---

## Test Execution

### Running Integration Tests

```bash
cd k8s-network-service-provider

# Health + registration smoke (manual today):
# - kind cluster + control-plane on :8081
# - export SP_* / DCM_REGISTRATION_URL, make run
# - curl http://localhost:8080/api/v1alpha1/networks/health

# Full integration suite (planned -- targets not in Makefile yet):
# make integration-test-up
# make test-integration
# make integration-test-down
```

Or with Ginkgo directly (when `test/integration` exists):

```bash
ginkgo -r test/integration --tags=integration
```

### Test Infrastructure Setup

Create `test/integration/setup.sh`:

```bash
#!/bin/bash
set -e

# Create Kind cluster
kind create cluster --name sp-test

# Create test namespace
kubectl create namespace dcm-test

# Start NATS
docker run -d --name nats-test -p 4222:4222 nats:2-alpine --jetstream

echo "Test infrastructure ready"
```

---

## Test Duration and Performance

- **Total suite duration:** < 5 minutes (with Kind cluster already running)
- **Individual test timeout:** 30 seconds max
- **Service creation latency:** < 2 seconds typical
- **Status propagation latency:** < 1 second to NATS

---

## Coverage Matrix

| Spec Requirement | Test Case IDs |
|------------------|---------------|
| REQ-HTTP-010 | TC-I001 |
| REQ-HTTP-020 | TC-I002 |
| REQ-HTTP-030 | TC-I003 |
| REQ-HTTP-040 | TC-I005 |
| REQ-HTTP-080 | TC-I004 |
| REQ-HLT-010 | TC-I002, TC-I011 |
| REQ-HLT-020 | TC-I011 |
| REQ-HLT-030 | TC-I011 |
| REQ-HLT-060 | TC-I091 |
| REQ-REG-010 | TC-I010 |
| REQ-REG-020 | TC-I010 |
| REQ-REG-040 | TC-I010 |
| REQ-REG-042 | TC-I010 |
| REQ-API-020 | TC-I020 |
| REQ-API-030 | TC-I027 |
| REQ-API-040 | TC-I028 |
| REQ-API-041 | TC-I028 |
| REQ-API-080 | TC-I024, TC-I031, TC-I032 |
| REQ-API-100 | TC-I025 |
| REQ-API-120 | TC-I034 |
| REQ-API-121 | TC-I037 |
| REQ-API-130 | TC-I036 |
| REQ-API-140 | TC-I030 |
| REQ-API-150 | TC-I033 |
| REQ-API-210 | TC-I050 |
| REQ-API-211 | TC-I050, TC-I052 |
| REQ-API-220 | TC-I051 |
| REQ-API-240 | TC-I021, TC-I022, TC-I023 |
| REQ-K8S-020 | TC-I020, TC-I026 |
| REQ-K8S-030 | TC-I092 |
| REQ-K8S-040 | TC-I020 |
| REQ-K8S-060 | TC-I035 |
| REQ-K8S-070 | TC-I080, TC-I082 |
| REQ-K8S-080 | TC-I020 |
| REQ-K8S-090 | TC-I021 |
| REQ-K8S-100 | TC-I022 |
| REQ-K8S-110 | TC-I023 |
| REQ-K8S-170 | TC-I031, TC-I032 |
| REQ-K8S-180 | TC-I036 |
| REQ-K8S-200 | TC-I092 |
| REQ-MON-010 | TC-I070 |
| REQ-MON-020 | TC-I070, TC-I074 |
| REQ-MON-060 | TC-I060, TC-I061, TC-I062 |
| REQ-MON-070 | TC-I063 |
| REQ-MON-080 | TC-I063 |
| REQ-MON-095 | TC-I063 |
| REQ-MON-120 | TC-I064 |
| REQ-MON-130 | TC-I065 |
| REQ-MON-175 | TC-I076 |
| REQ-MON-190 | TC-I075 |
| REQ-MON-210 | TC-I066 |
| REQ-MON-220 | TC-I090 |

---

## Notes

- Integration tests require Docker for Kind and NATS
- Tests should clean up resources after each run
- Use unique namespaces or resource names to avoid conflicts
- Tests can run in parallel where possible (use different Service names)
- Monitor resource usage to avoid overwhelming Kind cluster
