# 5G LMF — Location Management Function

A production-grade, 3GPP-compliant 5G Location Management Function implemented as a Go microservices monorepo with Kubernetes deployment.

> For architecture diagrams, flow charts, data-store schemas and algorithm details see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Repository Layout](#2-repository-layout)
3. [Quick Start](#3-quick-start)
4. [Building](#4-building)
5. [Running Locally](#5-running-locally)
6. [Testing](#6-testing)
7. [Docker Images](#7-docker-images)
8. [Kubernetes Deployment](#8-kubernetes-deployment)
9. [Helm Chart](#9-helm-chart)
10. [Configuration Reference](#10-configuration-reference)
11. [Proto / Code Generation](#11-proto--code-generation)
12. [Development Workflow](#12-development-workflow)
13. [Makefile Targets](#13-makefile-targets)
14. [Troubleshooting](#14-troubleshooting)

---

## 1. Prerequisites

| Tool | Minimum Version | Installation |
|------|----------------|--------------|
| Go | 1.22 | <https://go.dev/dl/> |
| Docker | 24 | <https://docs.docker.com/get-docker/> |
| Docker Compose | v2.24 | bundled with Docker Desktop |
| kubectl | 1.29 | <https://kubernetes.io/docs/tasks/tools/> |
| Helm | 3.14 | <https://helm.sh/docs/intro/install/> |
| protoc | 25 | <https://grpc.io/docs/protoc-installation/> |
| protoc-gen-go | v1.33 | `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest` |
| protoc-gen-go-grpc | v1.3 | `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest` |
| make | any | pre-installed on macOS / Linux |

Optional (for local infrastructure):

```
brew install redis kafka
brew install --cask docker
```

---

## 2. Repository Layout

```
lmf/
├── go.work                   # Go workspace — ties all modules together
├── go.work.sum
├── Makefile
├── README.md
├── ARCHITECTURE.md
│
├── common/                   # Shared library (one Go module)
│   ├── go.mod                # module github.com/5g-lmf/common
│   ├── clients/              # Redis, gRPC dial helpers
│   ├── config/               # Viper-based config loader
│   ├── middleware/           # Zap logger, Prometheus metrics, gRPC interceptors
│   └── types/                # All canonical domain types
│
├── proto/                    # Protobuf source
│   └── lmf/v1/
│       ├── session.proto
│       ├── location.proto
│       ├── positioning.proto
│       └── …
│
├── services/
│   ├── sbi-gateway/          # Nllmf HTTP/2 SBI gateway (Gin)
│   ├── location-request/     # Core LCS orchestrator
│   ├── session-manager/      # LCS session CRUD
│   ├── method-selector/      # Positioning method selection
│   ├── gnss-engine/          # A-GNSS positioning
│   ├── tdoa-engine/          # DL-TDOA positioning
│   ├── ecid-engine/          # NR E-CID positioning
│   ├── rtt-engine/           # Multi-RTT positioning
│   ├── fusion-engine/        # Multi-method position fusion
│   ├── qos-manager/          # QoS / accuracy evaluation
│   ├── event-manager/        # LCS event subscriptions
│   ├── privacy-auth/         # Privacy enforcement + JWT auth
│   ├── protocol-handler/     # LPP / NRPPa protocol translation
│   └── assistance-data/      # GNSS assistance data cache
│
└── deploy/
    ├── docker-compose.yaml   # Full local stack
    ├── k8s/                  # Raw Kubernetes manifests
    │   ├── 00-namespace.yaml
    │   ├── 01-configmap.yaml
    │   ├── 10-sbi-gateway.yaml
    │   └── … (15 files total)
    └── helm/
        └── lmf/              # Helm chart
            ├── Chart.yaml
            ├── values.yaml
            └── templates/
```

---

## 3. Quick Start

```bash
# 1. Clone the repo
git clone https://github.com/your-org/5g-lmf.git lmf
cd lmf

# 2. Sync the Go workspace
go work sync

# 3. Start local infrastructure (Redis, Kafka, Cassandra)
docker compose -f deploy/docker-compose.yaml up -d redis kafka cassandra

# 4. Build all services
make build

# 5. Run tests
make test

# 6. Start every service locally
make run
```

---

## 4. Building

### Build all services

```bash
make build
```

Binaries are placed in `bin/<service-name>`.

### Build a single service

```bash
cd services/session-manager
go build -o ../../bin/session-manager ./...
```

### Cross-compile for Linux (for Docker)

```bash
GOOS=linux GOARCH=amd64 go build -o bin/session-manager-linux ./services/session-manager/...
```

### Build with version info embedded

```bash
VERSION=$(git describe --tags --always)
go build \
  -ldflags "-X github.com/5g-lmf/common/middleware.Version=${VERSION}" \
  -o bin/location-request \
  ./services/location-request/...
```

---

## 5. Running Locally

### 5a. Infrastructure only (Docker Compose)

Start the external dependencies without any LMF services:

```bash
docker compose -f deploy/docker-compose.yaml up -d redis kafka cassandra
```

Services:
- Redis: `localhost:6379`
- Kafka: `localhost:9092`
- Cassandra: `localhost:9042`

### 5b. Start all LMF services natively

Each service reads `config/config.yaml` relative to its working directory and honours `LMF_`-prefixed environment variables.

```bash
# Session Manager
cd services/session-manager
LMF_REDIS_ADDR=localhost:6379 go run . &

# Method Selector
cd ../method-selector
go run . &

# Protocol Handler
cd ../protocol-handler
go run . &

# … repeat for all services
```

Or use the Makefile convenience target:

```bash
make run   # starts all services via 'go run' in background
```

### 5c. Start all services via Docker Compose

```bash
# Build images first
make docker-build

# Start everything (infra + services)
docker compose -f deploy/docker-compose.yaml up
```

View logs for a single service:

```bash
docker compose -f deploy/docker-compose.yaml logs -f location-request
```

### 5d. Environment variable overrides

Every `config.yaml` key maps to an env var with prefix `LMF_` and `_` replacing `.`:

| Config key | Environment variable |
|------------|---------------------|
| `redis.addr` | `LMF_REDIS_ADDR` |
| `grpc.port` | `LMF_GRPC_PORT` |
| `log.level` | `LMF_LOG_LEVEL` |
| `kafka.brokers` | `LMF_KAFKA_BROKERS` |
| `services.sessionManager` | `LMF_SERVICES_SESSIONMANAGER` |

---

## 6. Testing

### Run all tests

```bash
make test
```

This runs `go test ./...` in every module listed in `go.work`.

### Run tests for a single service

```bash
cd services/privacy-auth
go test ./...
```

### Run tests with race detector

```bash
go test -race ./...
```

### Run tests with coverage

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
open coverage.html
```

### Run a specific test

```bash
go test -run TestGeofenceEvaluator ./services/privacy-auth/internal/geofence/...
```

### Integration tests

Integration tests require running infrastructure. They are tagged with `//go:build integration`.

```bash
# Start infra
docker compose -f deploy/docker-compose.yaml up -d redis kafka cassandra

# Run integration tests
go test -tags=integration ./...
```

### Benchmark tests

```bash
go test -bench=. -benchmem ./services/gnss-engine/...
```

---

## 7. Docker Images

### Build all images

```bash
make docker-build
```

This calls `docker build` for each service using the `Dockerfile` in each service directory. Images are tagged as `lmf/<service-name>:latest`.

### Build a single image

```bash
docker build -t lmf/session-manager:latest ./services/session-manager
```

### Multi-platform build (for Kubernetes on ARM nodes)

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t your-registry/lmf/session-manager:v1.0.0 \
  --push \
  ./services/session-manager
```

### Push all images to registry

```bash
REGISTRY=your-registry.example.com make docker-push
```

### Image naming convention

```
<registry>/lmf/<service-name>:<git-tag>
```

Each Dockerfile follows a two-stage pattern:

```dockerfile
# Stage 1 — build
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /app/service ./...

# Stage 2 — runtime
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/service /service
ENTRYPOINT ["/service"]
```

---

## 8. Kubernetes Deployment

### Prerequisites

- A running Kubernetes cluster (1.29+)
- `kubectl` configured for the target cluster
- Namespace `lmf` created (or use the manifest below)
- Container images pushed to a registry accessible from the cluster

### Deploy raw manifests

```bash
# Create namespace + RBAC
kubectl apply -f deploy/k8s/00-namespace.yaml

# Apply ConfigMap (edit addresses first)
kubectl apply -f deploy/k8s/01-configmap.yaml

# Deploy infrastructure (StatefulSets)
kubectl apply -f deploy/k8s/20-redis-statefulset.yaml
kubectl apply -f deploy/k8s/21-kafka-statefulset.yaml
kubectl apply -f deploy/k8s/22-cassandra-statefulset.yaml

# Wait for infra to be ready
kubectl rollout status statefulset/redis -n lmf
kubectl rollout status statefulset/kafka -n lmf
kubectl rollout status statefulset/cassandra -n lmf

# Deploy application services
kubectl apply -f deploy/k8s/10-sbi-gateway.yaml
kubectl apply -f deploy/k8s/11-location-request.yaml
kubectl apply -f deploy/k8s/12-session-manager.yaml
kubectl apply -f deploy/k8s/13-method-selector.yaml
kubectl apply -f deploy/k8s/14-gnss-engine.yaml
kubectl apply -f deploy/k8s/15-tdoa-ecid-rtt.yaml
kubectl apply -f deploy/k8s/16-event-manager.yaml

# Apply network policies and monitoring
kubectl apply -f deploy/k8s/30-network-policy.yaml
kubectl apply -f deploy/k8s/40-monitoring.yaml
```

Apply everything at once:

```bash
kubectl apply -f deploy/k8s/
```

### Verify deployment

```bash
# Check all pods
kubectl get pods -n lmf

# Check services
kubectl get services -n lmf

# Follow logs for a deployment
kubectl logs -f deployment/location-request -n lmf

# Describe a pod for events/errors
kubectl describe pod -l app=session-manager -n lmf
```

### Scale a deployment

```bash
kubectl scale deployment/gnss-engine --replicas=5 -n lmf
```

### Rolling update

```bash
kubectl set image deployment/location-request \
  location-request=your-registry/lmf/location-request:v1.1.0 \
  -n lmf
kubectl rollout status deployment/location-request -n lmf
```

### Rollback

```bash
kubectl rollout undo deployment/location-request -n lmf
```

---

## 9. Helm Chart

The Helm chart at `deploy/helm/lmf/` manages the full application stack.

### Install

```bash
helm install lmf deploy/helm/lmf/ \
  --namespace lmf \
  --create-namespace \
  --values deploy/helm/lmf/values.yaml
```

### Install with custom values

```bash
helm install lmf deploy/helm/lmf/ \
  --namespace lmf \
  --create-namespace \
  --set global.imageRegistry=your-registry.example.com \
  --set global.imageTag=v1.0.0 \
  --set locationRequest.replicas=5
```

### Upgrade

```bash
helm upgrade lmf deploy/helm/lmf/ \
  --namespace lmf \
  --values deploy/helm/lmf/values.yaml \
  --set global.imageTag=v1.1.0
```

### Dry-run / template preview

```bash
helm template lmf deploy/helm/lmf/ \
  --values deploy/helm/lmf/values.yaml | less
```

### Uninstall

```bash
helm uninstall lmf --namespace lmf
```

### Key values

| Value | Default | Description |
|-------|---------|-------------|
| `global.imageRegistry` | `""` | Container registry prefix |
| `global.imageTag` | `latest` | Image tag applied to all services |
| `locationRequest.replicas` | `3` | Replicas for location-request |
| `redis.enabled` | `true` | Deploy bundled Redis |
| `kafka.enabled` | `true` | Deploy bundled Kafka |
| `externalRedis.host` | `""` | Use external Redis (disables bundled) |
| `services.amf.url` | `http://amf:8080` | AMF SBI endpoint |
| `services.udm.url` | `http://udm:8080` | UDM SBI endpoint |

---

## 10. Configuration Reference

Each service has a `config/config.yaml`. The shared structure (from `common/config/config.go`):

```yaml
grpc:
  port: 9090          # gRPC listen port
  addr: ""            # optional full address override (e.g. "0.0.0.0:9090")

http:
  port: 8080          # HTTP listen port (SBI gateway only)

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

kafka:
  brokers:
    - "localhost:9092"
  topic: "lmf-events"

cassandra:
  hosts:
    - "localhost:9042"
  keyspace: "lmf_audit"

log:
  level: "info"       # debug | info | warn | error
  format: "json"      # json | console

metrics:
  port: 2112          # Prometheus /metrics port

services:
  sessionManager:  "session-manager:9090"
  methodSelector:  "method-selector:9090"
  protocolHandler: "protocol-handler:9090"
  gnssEngine:      "gnss-engine:9090"
  tdoaEngine:      "tdoa-engine:9090"
  ecidEngine:      "ecid-engine:9090"
  rttEngine:       "rtt-engine:9090"
  fusionEngine:    "fusion-engine:9090"
  qosManager:      "qos-manager:9090"
  privacyAuth:     "privacy-auth:9090"

amf:
  baseURL: "http://amf.5gc.svc:8080"

udm:
  baseURL: "http://udm.5gc.svc:8080"
```

All keys can be overridden with `LMF_`-prefixed environment variables (Viper maps them automatically).

---

## 11. Proto / Code Generation

Proto files live in `proto/lmf/v1/`. Generated Go code is committed to `gen/go/`.

### Regenerate all protos

```bash
make proto
```

Which runs:

```bash
protoc \
  --go_out=gen/go \
  --go_opt=paths=source_relative \
  --go-grpc_out=gen/go \
  --go-grpc_opt=paths=source_relative \
  -I proto \
  proto/lmf/v1/*.proto
```

### Add a new RPC

1. Edit the relevant `.proto` file in `proto/lmf/v1/`
2. Run `make proto`
3. Implement the new method in the corresponding `internal/server/*.go`
4. Register the handler in `main.go`

---

## 12. Development Workflow

### Adding a new microservice

1. **Create the module directory**

   ```bash
   mkdir -p services/my-service/{internal/server,config}
   cd services/my-service
   go mod init github.com/5g-lmf/my-service
   ```

2. **Add to the workspace**

   ```
   # go.work — add a new `use` line
   use ./services/my-service
   ```

3. **Depend on common**

   ```bash
   go get github.com/5g-lmf/common
   ```

4. **Write the service** following existing patterns (`session-manager` is the cleanest reference).

5. **Add config**

   ```bash
   cp services/session-manager/config/config.yaml services/my-service/config/config.yaml
   # Edit ports / service name
   ```

6. **Add a Dockerfile**

   ```bash
   cp services/session-manager/Dockerfile services/my-service/Dockerfile
   # Edit the binary name on the last line
   ```

7. **Add Kubernetes manifest**

   ```bash
   cp deploy/k8s/12-session-manager.yaml deploy/k8s/17-my-service.yaml
   # Edit name, image, ports
   ```

8. **Register gRPC service** in `main.go` (see `session-manager/main.go` for the pattern).

### Running a single service in watch mode

```bash
# Install air (live reload)
go install github.com/air-verse/air@latest

cd services/session-manager
air
```

### Linting

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

make lint
# or per-service:
golangci-lint run ./services/session-manager/...
```

### Formatting

```bash
make fmt
# or:
gofmt -w ./...
goimports -w ./...
```

### Updating dependencies

```bash
# Update all modules in the workspace
go work sync
# Update a specific dep in one module
cd common && go get github.com/redis/go-redis/v9@latest && go mod tidy
```

---

## 13. Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Compile all service binaries to `bin/` |
| `make test` | Run all unit tests |
| `make test-race` | Run tests with `-race` detector |
| `make test-cover` | Generate HTML coverage report |
| `make lint` | Run golangci-lint across all modules |
| `make fmt` | Format code with gofmt + goimports |
| `make proto` | Regenerate gRPC/proto Go code |
| `make docker-build` | Build all Docker images |
| `make docker-push` | Push images to `$REGISTRY` |
| `make deploy` | `kubectl apply -f deploy/k8s/` |
| `make helm-install` | `helm install lmf deploy/helm/lmf/` |
| `make helm-upgrade` | `helm upgrade lmf deploy/helm/lmf/` |
| `make run` | Start all services locally (background) |
| `make stop` | Kill locally running services |
| `make clean` | Remove `bin/` and build cache |

---

## 14. Troubleshooting

### `go: module not found` after adding a new service

```bash
go work sync
```

### gRPC dial fails with `connection refused`

Check the `services.*` config section — addresses must include the port:

```yaml
services:
  sessionManager: "localhost:9091"
```

Or set via env:

```bash
LMF_SERVICES_SESSIONMANAGER=localhost:9091
```

### Redis `WRONGTYPE` error

The `supi-subs:<supi>` key is a Redis Set. If it was accidentally created as a String (e.g. by a previous bug), delete it:

```bash
redis-cli DEL supi-subs:imsi-001010000000001
```

### Cassandra `NoHostAvailable`

The Cassandra keyspace may not exist yet. Create it:

```sql
CREATE KEYSPACE IF NOT EXISTS lmf_audit
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 3};
```

### Pod in `CrashLoopBackOff`

```bash
kubectl logs <pod-name> -n lmf --previous
```

Common causes:
- `config.yaml` not mounted or wrong path → check ConfigMap and volume mounts
- Redis/Kafka not ready → add `initContainers` that wait for deps
- Missing env vars (e.g. `JWT_SIGNING_KEY`) → add to Secret and mount

### Metrics not appearing in Prometheus

Verify the metrics port is exposed and the ServiceMonitor label matches your Prometheus operator:

```bash
kubectl get servicemonitor -n lmf
kubectl get service -n lmf -o wide
curl http://<pod-ip>:2112/metrics
```

### Running out of file descriptors (local dev)

Each gRPC server keeps connections open. Raise the limit:

```bash
ulimit -n 65535
```

---

## License

Apache 2.0 — see [LICENSE](LICENSE).

## Contributing

1. Fork → feature branch → `make fmt lint test` → PR.
2. Follow the [Conventional Commits](https://www.conventionalcommits.org/) spec for commit messages.
3. Every PR must pass CI (`make test-race lint`) before merge.
