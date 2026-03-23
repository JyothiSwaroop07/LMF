# 5G LMF — Architecture Document

> 3GPP TS 23.273 / TS 29.572 compliant Location Management Function
> Microservices monorepo · Go 1.22 · gRPC · Kubernetes

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [3GPP Standards Compliance](#2-3gpp-standards-compliance)
3. [Repository Layout](#3-repository-layout)
4. [Microservice Catalogue](#4-microservice-catalogue)
5. [Architecture Diagrams](#5-architecture-diagrams)
   - 5.1 [High-Level 5G Core Integration](#51-high-level-5g-core-integration)
   - 5.2 [LMF Internal Service Mesh](#52-lmf-internal-service-mesh)
   - 5.3 [Location Request End-to-End Flow](#53-location-request-end-to-end-flow)
   - 5.4 [Positioning Method Selection Tree](#54-positioning-method-selection-tree)
   - 5.5 [Privacy Enforcement Flow](#55-privacy-enforcement-flow)
   - 5.6 [Event Subscription Lifecycle](#56-event-subscription-lifecycle)
   - 5.7 [GNSS Engine Data Flow](#57-gnss-engine-data-flow)
   - 5.8 [TDOA Engine Data Flow](#58-tdoa-engine-data-flow)
   - 5.9 [Fusion Engine Pipeline](#59-fusion-engine-pipeline)
   - 5.10 [Kubernetes Deployment Topology](#510-kubernetes-deployment-topology)
6. [Inter-Service Communication](#6-inter-service-communication)
7. [Data Stores](#7-data-stores)
8. [Key Algorithms](#8-key-algorithms)
9. [Common Package (`github.com/5g-lmf/common`)](#9-common-package)
10. [Proto Definitions](#10-proto-definitions)
11. [Security Architecture](#11-security-architecture)
12. [Observability](#12-observability)
13. [Configuration Reference](#13-configuration-reference)
14. [File-by-File Reference](#14-file-by-file-reference)

---

## 1. System Overview

The **5G Location Management Function (LMF)** is a 5G Core Network function defined in
3GPP TS 23.273. It provides **UE positioning services** to authorized LCS Clients
(emergency services, value-added services, PLMN operators, lawful intercept).

This implementation decomposes the LMF into **14 focused microservices** that communicate
via gRPC, share state via Redis Cluster, stream events via Apache Kafka, and persist audit
data in Apache Cassandra.

```
LCS Client / GMLC
       │
       │  Nllmf (HTTP/2, TS 29.572)
       ▼
  ┌─────────────────────────────────────────────────────────────────┐
  │                    LMF Microservices                            │
  │  SBI Gateway → Location Request → [14 specialized services]    │
  └─────────────────────────────────────────────────────────────────┘
       │                    │
  LPP (N1/AMF)        NRPPa (N2/AMF)
       │                    │
      UE                   gNB
```

**Key properties:**
- All internal communication is **gRPC** (HTTP/2, protobuf)
- The external Nllmf interface is **REST/HTTP/2 JSON** (TS 29.572)
- Each service is independently deployable, scalable, and testable
- All services emit **Prometheus metrics** and **structured JSON logs**

---

## 2. 3GPP Standards Compliance

| Standard | Description | Implemented In |
|----------|-------------|----------------|
| TS 23.273 | 5GS Location Services Architecture | All services |
| TS 29.572 | Nllmf SBI API (HTTP/2 REST) | `sbi-gateway` |
| TS 38.305 | NR Positioning Stage 2 (procedures) | `location-request`, `protocol-handler` |
| TS 38.455 | NRPPa Protocol (gNB ↔ LMF) | `protocol-handler/internal/nrppa` |
| TS 36.355 | LPP Protocol (UE ↔ LMF) | `protocol-handler/internal/lpp` |
| TS 23.032 | GAD Shapes (geographic area description) | `common/types/lcs_types.go` |
| TS 23.501 | 5GS Architecture (AMF, UDM interfaces) | `privacy-auth`, `protocol-handler` |
| TS 33.127 | LCS Security (audit logging) | `privacy-auth/internal/audit` |
| IS-GPS-200N | GPS Interface Control Document | `gnss-engine/internal/positioning` |

---

## 3. Repository Layout

```
lmf/
├── go.work                         # Go workspace (ties all modules together)
├── Makefile                        # Build, test, lint, docker, deploy targets
│
├── common/                         # Shared library (github.com/5g-lmf/common)
│   ├── go.mod
│   ├── types/
│   │   ├── lcs_types.go            # Core LCS domain types (LcsSession, LcsQoS, ...)
│   │   ├── gnss_types.go           # GNSS types (GnssEphemeris, KlobucharModel, ...)
│   │   └── positioning_types.go    # Positioning types (PositionEstimate, EcidMeasurements, ...)
│   ├── config/
│   │   └── config.go               # Unified config struct + Viper loader
│   ├── middleware/
│   │   ├── logging.go              # Zap logger + gRPC interceptors
│   │   └── metrics.go              # All Prometheus metric definitions
│   └── clients/
│       ├── redis_client.go         # Redis Cluster client helpers
│       └── kafka_client.go         # Kafka producer/consumer (Sarama)
│
├── proto/                          # Protocol Buffer definitions
│   ├── lmf_common.proto            # Shared enums and messages
│   ├── lmf_location.proto          # Location request/session services
│   ├── lmf_positioning.proto       # Positioning engine services
│   ├── lmf_events.proto            # Event subscription / privacy services
│   └── lmf_protocol.proto          # LPP/NRPPa protocol handler service
│
├── services/
│   ├── sbi-gateway/                # MS-01: Nllmf HTTP/2 REST ↔ gRPC gateway
│   ├── location-request/           # MS-02: End-to-end orchestrator
│   ├── session-manager/            # MS-03: LCS session lifecycle (Redis)
│   ├── protocol-handler/           # MS-04: LPP + NRPPa message handler
│   ├── method-selector/            # MS-05: Positioning method decision engine
│   ├── gnss-engine/                # MS-06: A-GNSS WLS positioning
│   ├── tdoa-engine/                # MS-07: DL-TDOA Chan's algorithm
│   ├── ecid-engine/                # MS-08: NR E-CID (TA + RSRP centroid)
│   ├── rtt-engine/                 # MS-09: Multi-RTT WLS multilateration
│   ├── fusion-engine/              # MS-10: Inverse-variance + EKF fusion
│   ├── assistance-data/            # MS-11: GNSS assistance data cache/provider
│   ├── event-manager/              # MS-12: Geofence / motion event subscriptions
│   ├── privacy-auth/               # MS-13: LCS privacy enforcement + JWT auth
│   └── qos-manager/                # MS-14: QoS accuracy + response time evaluation
│
└── deploy/
    ├── k8s/                        # Raw Kubernetes YAML manifests
    │   ├── 00-namespace.yaml
    │   ├── 01-configmap.yaml
    │   ├── 02-secrets.yaml
    │   ├── 10-sbi-gateway.yaml
    │   ├── 11-location-request.yaml
    │   ├── 12-session-manager.yaml
    │   ├── 13-protocol-handler.yaml
    │   ├── 14-method-selector.yaml
    │   ├── 15-positioning-engines.yaml  # gnss/tdoa/ecid/rtt/fusion
    │   ├── 16-support-services.yaml     # assistance/event/privacy/qos
    │   ├── 20-redis-statefulset.yaml
    │   ├── 21-kafka-statefulset.yaml
    │   ├── 22-cassandra-statefulset.yaml
    │   ├── 30-network-policy.yaml
    │   └── 40-monitoring.yaml
    └── helm/lmf/                   # Helm chart
        ├── Chart.yaml
        ├── values.yaml
        └── templates/
            ├── _helpers.tpl
            ├── sbi-gateway.yaml
            └── services.yaml       # Template loop for all 13 gRPC services
```

Each service follows this internal layout:

```
services/<name>/
├── go.mod                          # Module: github.com/5g-lmf/<name>
├── main.go                         # Entry point: config → init → gRPC serve
├── config/config.yaml              # Default configuration
├── Dockerfile                      # Multi-stage build (builder + alpine runtime)
└── internal/
    ├── <domain>/                   # Core business logic (pure, testable)
    └── server/                     # gRPC server implementation
```

---

## 4. Microservice Catalogue

| # | Name | Module | gRPC Port | Metrics Port | Primary Responsibility |
|---|------|--------|-----------|--------------|------------------------|
| MS-01 | sbi-gateway | `5g-lmf/sbi-gateway` | 9000 | 2110 | Nllmf HTTP/2 REST ↔ gRPC translation, JWT validation |
| MS-02 | location-request | `5g-lmf/location-request` | 9001 | 2111 | End-to-end location request orchestrator |
| MS-03 | session-manager | `5g-lmf/session-manager` | 9002 | 2112 | LCS session create/read/update/delete in Redis |
| MS-04 | protocol-handler | `5g-lmf/protocol-handler` | 9003 | 2113 | LPP (N1) and NRPPa (N2) message encode/deliver |
| MS-05 | method-selector | `5g-lmf/method-selector` | 9004 | 2114 | QoS + capability-based method selection |
| MS-06 | gnss-engine | `5g-lmf/gnss-engine` | 9005 | 2115 | A-GNSS Keplerian mechanics + WLS fix |
| MS-07 | tdoa-engine | `5g-lmf/tdoa-engine` | 9006 | 2116 | DL-TDOA Chan's two-stage WLS |
| MS-08 | ecid-engine | `5g-lmf/ecid-engine` | 9007 | 2117 | NR E-CID (TA range + RSRP path loss centroid) |
| MS-09 | rtt-engine | `5g-lmf/rtt-engine` | 9008 | 2118 | Multi-RTT WLS range multilateration |
| MS-10 | fusion-engine | `5g-lmf/fusion-engine` | 9009 | 2119 | Inverse-variance weighted fusion + EKF tracking |
| MS-11 | assistance-data | `5g-lmf/assistance-data` | 9010 | 2120 | GNSS ephemeris/iono cache (Redis TTL-based) |
| MS-12 | event-manager | `5g-lmf/event-manager` | 9011 | 2121 | Area/motion event subscriptions + HTTP notify |
| MS-13 | privacy-auth | `5g-lmf/privacy-auth` | 9012 | 2122 | CLASS A/B/C/D privacy + JWT scope check + Cassandra audit |
| MS-14 | qos-manager | `5g-lmf/qos-manager` | 9013 | 2123 | Accuracy fulfilment + response time evaluation |

---

## 5. Architecture Diagrams

### 5.1 High-Level 5G Core Integration

```
┌──────────────────────────────────────────────────────────────┐
│                        5G Core Network                       │
│                                                              │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐   │
│  │  GMLC   │    │   AMF   │    │   UDM   │    │   NRF   │   │
│  └────┬────┘    └────┬────┘    └────┬────┘    └────┬────┘   │
│       │              │              │               │        │
│  Nllmf│         N1/N2│        Nudm  │         Nnrf  │        │
│  (HTTP/2)      (HTTP/2)     (HTTP/2)       (HTTP/2) │        │
└───────┼──────────────┼──────────────┼───────────────┼────────┘
        │              │              │               │
        ▼              │              │               │
  ┌─────────────┐      │              │               │
  │ SBI Gateway │◄─────┘              │               │
  │  (MS-01)    │                     │               │
  └──────┬──────┘                     │               │
         │ gRPC                       │               │
         ▼                            │               │
  ┌─────────────────┐                 │               │
  │ Location Request│                 │               │
  │   (MS-02)       │─────────────────┘               │
  │   Orchestrator  │  (UDM privacy via MS-13)        │
  └────────┬────────┘                                 │
           │ gRPC to all downstream services          │
           │                                          │
     ┌─────┴──────────────────────────────────┐       │
     │     LMF Internal Services              │       │
     │  MS-03 MS-04 MS-05 MS-06...MS-14       │◄──────┘
     └────────────────────────────────────────┘
                                    (NRF registration via MS-01)

Radio Access Network (RAN):
  Protocol Handler (MS-04) ──LPP──► UE    (via AMF N1)
  Protocol Handler (MS-04) ──NRPPa──► gNB (via AMF N2)
```

### 5.2 LMF Internal Service Mesh

```
                    ┌─────────────────────────────────────────────┐
                    │           LMF Service Mesh (Istio)          │
                    │                                             │
  HTTP/2 REST       │  ┌─────────────┐                           │
  ──────────────►   │  │ sbi-gateway │  JWT Validation           │
                    │  │   MS-01     │  Rate Limiting             │
                    │  └──────┬──────┘  TLS Termination          │
                    │         │ gRPC                             │
                    │         ▼                                   │
                    │  ┌─────────────────┐                        │
                    │  │ location-request│◄── Event subs (MS-12) │
                    │  │    MS-02        │                        │
                    │  │  Orchestrator   │                        │
                    │  └────────┬────────┘                        │
                    │     gRPC  │  (fan-out)                      │
                    │    ┌──────┼──────────────────────┐          │
                    │    │      │                      │          │
                    │    ▼      ▼                      ▼          │
                    │ ┌──────┐ ┌──────────┐ ┌─────────────────┐  │
                    │ │MS-03 │ │  MS-04   │ │    MS-05        │  │
                    │ │Sess. │ │Protocol  │ │Method Selector  │  │
                    │ │Mgr   │ │Handler   │ │                 │  │
                    │ └──────┘ └──────────┘ └────────┬────────┘  │
                    │                                │            │
                    │              ┌─────────────────┤            │
                    │              │                 │            │
                    │         ┌────┴──────────────────┐           │
                    │         │  Positioning Engines   │           │
                    │         │  MS-06 MS-07 MS-08     │           │
                    │         │  MS-09  ──►  MS-10     │           │
                    │         └───────────────────────┘           │
                    │                                             │
                    │  ┌──────────┐ ┌──────────┐ ┌──────────┐    │
                    │  │  MS-11   │ │  MS-13   │ │  MS-14   │    │
                    │  │Assist.   │ │Privacy   │ │QoS Mgr   │    │
                    │  │Data      │ │Auth      │ │          │    │
                    │  └──────────┘ └──────────┘ └──────────┘    │
                    └─────────────────────────────────────────────┘

Shared Infrastructure:
  ┌──────────────┐  ┌──────────────┐  ┌────────────────┐
  │ Redis Cluster│  │Apache Kafka  │  │Apache Cassandra│
  │(session/cache│  │(event stream)│  │(audit/geometry)│
  └──────────────┘  └──────────────┘  └────────────────┘
```

### 5.3 Location Request End-to-End Flow

```
LCS Client          SBI Gateway      Location Request          Services
    │                  (MS-01)          (MS-02)
    │                     │
    │ POST /nlmf-loc/v1/  │
    │  location-contexts  │
    │─────────────────────►
    │                     │  gRPC DetermineLocation
    │                     │─────────────────────────►
    │                     │                          │
    │                     │        ┌─────────────────┼──────────────────────┐
    │                     │        │  1. Privacy Check│                      │
    │                     │        │     ┌────────────▼──────────┐           │
    │                     │        │     │  privacy-auth (MS-13) │           │
    │                     │        │     │  ├─ Validate JWT token│           │
    │                     │        │     │  ├─ Fetch UDM profile │           │
    │                     │        │     │  ├─ CLASS A: bypass   │           │
    │                     │        │     │  ├─ CLASS B: notify   │           │
    │                     │        │     │  ├─ CLASS C: consent  │           │
    │                     │        │     │  └─ CLASS D: deny     │           │
    │                     │        │     └───────────────────────┘           │
    │                     │        │                                         │
    │                     │        │  2. Session Create                      │
    │                     │        │     session-manager (MS-03) → Redis     │
    │                     │        │                                         │
    │                     │        │  3. UE Capabilities                     │
    │                     │        │     protocol-handler (MS-04)            │
    │                     │        │       └─ LPP RequestCapabilities → UE   │
    │                     │        │                                         │
    │                     │        │  4. Method Selection                    │
    │                     │        │     method-selector (MS-05)             │
    │                     │        │       └─ QoS + caps → method tree       │
    │                     │        │                                         │
    │                     │        │  5. Measurement Trigger                 │
    │                     │        │     protocol-handler (MS-04)            │
    │                     │        │       ├─ LPP → UE (N1 via AMF)          │
    │                     │        │       └─ NRPPa → gNB (N2 via AMF)       │
    │                     │        │                                         │
    │                     │        │  6. Positioning Computation             │
    │                     │        │     ┌─────────┬────────┬────────┐       │
    │                     │        │     │gnss-eng │tdoa-eng│ecid-eng│       │
    │                     │        │     │(MS-06)  │(MS-07) │(MS-08) │       │
    │                     │        │     └────┬────┴───┬────┴────┬───┘       │
    │                     │        │          └────────┼─────────┘           │
    │                     │        │                   ▼                     │
    │                     │        │  7. Fusion (if multiple estimates)      │
    │                     │        │     fusion-engine (MS-10)               │
    │                     │        │       ├─ Inverse-variance weighted avg  │
    │                     │        │       └─ EKF Kalman filter update       │
    │                     │        │                                         │
    │                     │        │  8. QoS Evaluation                      │
    │                     │        │     qos-manager (MS-14)                 │
    │                     │        │       └─ AccuracyFulfilmentIndicator     │
    │                     │        │                                         │
    │                     │        │  9. Session Close + Return              │
    │                     │        └─────────────────────────────────────────┘
    │                     │
    │ 200 OK              │
    │  LocationEstimate   │
    │◄────────────────────│
```

### 5.4 Positioning Method Selection Tree

```
MethodSelectionRequest
{UeCaps, LcsQoS, IndoorHint}
          │
          ▼
    ResponseTime?
    ┌────────────────────────────────────────────────────┐
    │                                                    │
    ▼                           ▼                        ▼
NoDelay (≤500ms)         LowDelay (≤1s)          DelayTolerant (≤10/30s)
    │                           │                        │
    ▼                           ▼                        ▼
EcidSupported?          MultiRTT + ECID          IndoorHint?
  Yes ──► NR E-CID       (fallback chain)         Yes ──► WLAN → BLE → Baro
  No  ──► Cell-ID                                 No ──► HorizontalAccuracy?
                                                     │
                                        ┌────────────┼────────────┐
                                        │            │            │
                                       ≤10m        ≤50m        >50m
                                        │            │            │
                                        ▼            ▼            ▼
                                   GnssSupported? TDOA+RTT   TDOA+RTT
                                   DlTdoa?           │         │
                                   ├─Yes: GNSS       │         │
                                   │  +TDOA+WLAN     │         │
                                   └─No: TDOA        │         │
                                                     │         │
                              FallbackMethods: ◄─────┴─────────┘
                              Built from remaining supported
                              methods sorted by accuracy
```

### 5.5 Privacy Enforcement Flow

```
CheckPrivacy(supi, sessionId, lcsClientType)
           │
           ▼
  LcsClientType == EMERGENCY_SERVICES ?
  ├─ Yes ──► CLASS_A: ALLOWED (bypass) ──────────────────────────────────┐
  │                                                                       │
  LcsClientType == LAWFUL_INTERCEPT ?                                     │
  ├─ Yes ──► CLASS_A: ALLOWED (logged)  ──────────────────────────────────┤
  │                                                                       │
  Fetch UDM Privacy Profile (Nudm_SDM)                                    │
  │   └─ On failure: use defaultClass (CLASS_C)                           │
  │                                                                       │
  PrivacyClass?                                                           │
  ┌─────┬─────────┬──────────────────┬───────────────────┐               │
  │     │         │                  │                   │               │
  ▼     ▼         ▼                  ▼                   ▼               │
CLASS_A │     CLASS_B           CLASS_C             CLASS_D               │
Allow  │  Notify async       Notify sync           Deny                  │
  │    │  Allow immediately  Wait for consent        │                   │
  │    │  (fire-and-forget)  (configurable timeout)  │                   │
  │    │       │              │      │                │                   │
  │    │       │             Yes    No/Timeout        │                   │
  │    │       │              │      │                │                   │
  │    │    ALLOWED       ALLOWED  DENIED          DENIED                 │
  └────┴───────┴──────────────┴──────┴────────────────┴───────────────────┘
                                   │
                                   ▼
                        Write Cassandra Audit Record
                        (TS 33.127: supi, sessionId, class, outcome)
```

### 5.6 Event Subscription Lifecycle

```
Subscriber                 SBI Gateway              Event Manager
    │                         │                        │
    │ POST /nlmf-loc/v1/      │                        │
    │  subscriptions          │                        │
    │────────────────────────►│                        │
    │                         │  gRPC Subscribe        │
    │                         │───────────────────────►│
    │                         │                        │ Save to Redis
    │                         │                        │  (sub:uuid, supi-subs:supi)
    │◄────────────────────────│◄───────────────────────│
    │ 201 Created             │  SubscriptionID        │
    │ {subscriptionId}        │                        │
    │                         │                        │
    │                         │     EventScheduler (every 10s)
    │                         │                        │
    │                         │     ┌───────────────────┤
    │                         │     │ Fetch last pos    │
    │                         │     │ EvaluateAreaEvent │
    │                         │     │  (ray-casting)    │
    │                         │     │ EvaluateMotion    │
    │                         │     │  (Haversine)      │
    │                         │     └───────────────────┤
    │                         │                        │
    │                         │  Triggered?            │
    │◄──────────────────────────────────────────────── │
    │ POST {notifUri}          │  HTTP POST notify      │
    │ EventNotification JSON  │                        │
    │                         │                        │
    │ DELETE /subscriptions/  │                        │
    │  {id}                   │                        │
    │────────────────────────►│  gRPC Unsubscribe      │
    │                         │───────────────────────►│ Delete from Redis
    │◄────────────────────────│◄───────────────────────│
    │ 204 No Content          │                        │
```

### 5.7 GNSS Engine Data Flow

```
GnssComputeRequest
{SessionID, Measurements[], Ephemerides[]}
           │
           ▼
  ┌────────────────────────────────────────────────────────┐
  │              gnss_solver.go  (IS-GPS-200N)             │
  │                                                        │
  │  For each SV measurement:                              │
  │  ┌─────────────────────────────────────────────────┐   │
  │  │  1. Solve Kepler Equation (Newton-Raphson)       │   │
  │  │     E(k+1) = M + e·sin(E(k))  until |ΔE| < 1e-12│   │
  │  │                                                  │   │
  │  │  2. Compute orbital position (true anomaly)      │   │
  │  │     ν = atan2(√(1-e²)·sin(E), cos(E)-e)         │   │
  │  │                                                  │   │
  │  │  3. Apply 6 harmonic perturbations               │   │
  │  │     δu = Cus·sin2φ + Cuc·cos2φ  (arg. of lat)   │   │
  │  │     δr = Crs·sin2φ + Crc·cos2φ  (radius)        │   │
  │  │     δi = Cis·sin2φ + Cic·cos2φ  (inclination)   │   │
  │  │                                                  │   │
  │  │  4. ECEF position (with Sagnac correction)       │   │
  │  │     Rotate by ωe·τ (Earth rotation during flight)│   │
  │  │                                                  │   │
  │  │  5. Relativistic clock correction                │   │
  │  │     Δtr = -2√(μa)/c² · e·sin(E)                 │   │
  │  └─────────────────────────────────────────────────┘   │
  │                                                        │
  │  WLS Iterative Solver (4 unknowns: x, y, z, dt)        │
  │  ┌─────────────────────────────────────────────────┐   │
  │  │  H · Δx = δρ                                    │   │
  │  │  H = [direction cosines | 1]  (n×4)             │   │
  │  │  W = diag(1/σ²_i)  (elevation-based weights)    │   │
  │  │  Δx = (HᵀWH)⁻¹ HᵀW δρ  (4×4 Gaussian elim.)   │   │
  │  │  Iterate until ‖Δx‖ < 1mm                       │   │
  │  └─────────────────────────────────────────────────┘   │
  │                                                        │
  │  ECEF → Geodetic (Zhu's closed-form)                   │
  │  ┌─────────────────────────────────────────────────┐   │
  │  │  p = √(x²+y²), θ = atan2(z·a, p·b)             │   │
  │  │  φ = atan2(z + e'²b·sin³θ, p - e²a·cos³θ)      │   │
  │  └─────────────────────────────────────────────────┘   │
  └────────────────────────────────────────────────────────┘
           │
           ▼
  PositionEstimate{Latitude, Longitude, Altitude,
                   HorizontalAccuracy, HDOP, NumSatellites}
```

### 5.8 TDOA Engine Data Flow

```
DlTdoaRequest
{RSTD Measurements[], Cell Geometries[]}
          │
          ▼
  ┌────────────────────────────────────────────────────────┐
  │              tdoa_solver.go  (Chan's Algorithm)        │
  │                                                        │
  │  1. Convert RSTD → range difference (metres)           │
  │     Δdᵢ = RSTDᵢ × Ts × c    (Ts = 1/30.72MHz)         │
  │                                                        │
  │  2. Set up hyperbolic TDOA equations                   │
  │     ‖r - rᵢ‖ - ‖r - r₀‖ = Δdᵢ  (ref = cell 0)        │
  │                                                        │
  │  3. Chan's Stage 1 — Linear WLS                        │
  │     Linearise by squaring both sides → matrix form     │
  │     Ψ = [−x₁₀  −y₁₀  Δd₁ ]                           │
  │         [−x₂₀  −y₂₀  Δd₂ ]  ...                      │
  │     h = [½(Δd₁² − R₁² + R₀²)]  ...                   │
  │     z₁ = (ΨᵀQΨ)⁻¹ Ψᵀ Q h  (coarse fix + ref range)   │
  │                                                        │
  │  4. Chan's Stage 2 — Refinement WLS                    │
  │     Use z₁ to build improved weight matrix B           │
  │     z₂ = (BᵀΨ₂B)⁻¹ Bᵀ Ψ₂ h₂  (refined fix)           │
  │                                                        │
  │  5. ENU → Geodetic                                     │
  │     Convert ENU offset from ref cell to lat/lon        │
  │                                                        │
  │  6. HDOP computation from geometry matrix              │
  └────────────────────────────────────────────────────────┘
          │
          ▼
  PositionEstimate{Latitude, Longitude, SigmaLat, SigmaLon}
```

### 5.9 Fusion Engine Pipeline

```
Multiple PositionEstimates (from different engines)
           │
           ▼
  ┌────────────────────────────────────────────────────────┐
  │              weighted_fusion.go                        │
  │                                                        │
  │  1. Outlier Rejection                                  │
  │     Compute median σ across all estimates              │
  │     Reject estimates where σ > 3× median σ             │
  │                                                        │
  │  2. Inverse-Variance Weighted Average                  │
  │     wᵢ = 1/σᵢ²                                        │
  │     lat_fused = Σ(wᵢ × latᵢ) / Σwᵢ                   │
  │     σ_fused = 1/√(Σwᵢ)                                │
  └────────────────────────────────────────────────────────┘
           │
           ▼
  ┌────────────────────────────────────────────────────────┐
  │              kalman_filter.go  (6-state EKF)           │
  │                                                        │
  │  State vector: x = [lat, lon, alt, vLat, vLon, vAlt]  │
  │                                                        │
  │  Predict:                                              │
  │    x⁻ = F·x  (constant velocity model)                │
  │    P⁻ = F·P·Fᵀ + Q  (process noise)                   │
  │                                                        │
  │  Update:                                               │
  │    K = P⁻·Hᵀ·(H·P⁻·Hᵀ + R)⁻¹  (Kalman gain)          │
  │    x = x⁻ + K·(z - H·x⁻)       (state update)        │
  │    P = (I - K·H)·P⁻              (covariance update)  │
  │                                                        │
  │  Tracks per-SUPI; TTL cleanup every 5 minutes          │
  └────────────────────────────────────────────────────────┘
           │
           ▼
  PositionEstimate{Latitude, Longitude, SigmaLat, SigmaLon}
  QualityIndex = 100 × (1 - clamp(σ_metres/1000))
```

### 5.10 Kubernetes Deployment Topology

```
┌─────────────────────────── Kubernetes Cluster ──────────────────────────────┐
│                                                                              │
│  ┌─────── Namespace: lmf ────────────────────────────────────────────────┐  │
│  │                                                                       │  │
│  │  ┌──────────────────────────── Istio Service Mesh ─────────────────┐  │  │
│  │  │                                                                 │  │  │
│  │  │  sbi-gateway (2 pods, HPA 2-10)                                │  │  │
│  │  │  ─────────────────────────────────────────────────────────     │  │  │
│  │  │  location-request (3 pods, HPA 3-20)                           │  │  │
│  │  │  ─────────────────────────────────────────────────────────     │  │  │
│  │  │  session-manager  method-selector  protocol-handler  (2 each)  │  │  │
│  │  │  ─────────────────────────────────────────────────────────     │  │  │
│  │  │  gnss-engine  tdoa-engine  ecid-engine  rtt-engine  (2 each)   │  │  │
│  │  │  ─────────────────────────────────────────────────────────     │  │  │
│  │  │  fusion-engine  assistance-data  event-manager  (2 each)       │  │  │
│  │  │  ─────────────────────────────────────────────────────────     │  │  │
│  │  │  privacy-auth  qos-manager  (2 each)                           │  │  │
│  │  │                                                                 │  │  │
│  │  └─────────────────────────────────────────────────────────────────┘  │  │
│  │                                                                       │  │
│  │  ┌──── StatefulSets ──────────────────────────────────────────────┐  │  │
│  │  │  Redis Cluster (3 nodes)  │  Kafka (3 brokers)  │  Cassandra(3)│  │  │
│  │  └───────────────────────────────────────────────────────────────┘  │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌──── Namespace: monitoring ─────────────────────────────────────────────┐  │
│  │  Prometheus  │  Grafana  │  Jaeger                                     │  │
│  └─────────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Inter-Service Communication

### gRPC Service Contracts

All internal communication uses gRPC with Protocol Buffers. The proto files define
the contracts:

| Proto File | Services Defined |
|------------|-----------------|
| `lmf_location.proto` | `LocationRequestService`, `SessionManagerService`, `MethodSelectorService`, `QosManagerService` |
| `lmf_positioning.proto` | `GnssEngineService`, `TdoaEngineService`, `EcidEngineService`, `RttEngineService`, `FusionEngineService` |
| `lmf_events.proto` | `EventManagerService`, `PrivacyAuthService` |
| `lmf_protocol.proto` | `ProtocolHandlerService` |
| `lmf_common.proto` | Shared message types |

### Service Discovery

- Production: Kubernetes DNS (`<service-name>.<namespace>.svc.cluster.local`)
- Service addresses configured via `config.yaml` → `services:` block
- NRF registration via `sbi-gateway` on startup (TS 29.510 `Nnrf_NFManagement_NFRegister`)

### gRPC Interceptors (from `common/middleware`)

```
GrpcLoggingInterceptor   →  Request/response structured logging (Zap)
GrpcStreamLoggingInterceptor → Streaming RPC logging
```

---

## 7. Data Stores

### Redis Cluster (Sessions + Assistance Data Cache)

| Key Pattern | Content | TTL | Used By |
|-------------|---------|-----|---------|
| `sess:{sessionId}` | `LcsSession` JSON | 5 min | session-manager |
| `supi:{supi}` | Set of session IDs | 5 min | session-manager |
| `sub:{subId}` | `EventSubscription` JSON | 24 h | event-manager |
| `supi-subs:{supi}` | Set of subscription IDs | 24 h | event-manager |
| `assist:ephem:{constellation}` | `[]GnssEphemeris` JSON | 2 h | assistance-data |
| `assist:iono:klobuchar` | `KlobucharModel` JSON | 1 h | assistance-data |
| `assist:refloc:{areaId}` | `{lat, lon, uncertainty}` JSON | 30 min | assistance-data |
| `cell:{nci}` | `CellGeometry` JSON | varies | tdoa-engine |

### Apache Cassandra (Audit + Cell Geometry)

| Keyspace | Table | Content | Used By |
|----------|-------|---------|---------|
| `lmf_audit` | `lcs_audit` | Privacy decision records (TS 33.127) | privacy-auth |
| `lmf_geometry` | `cell_geometry` | gNB/TRP positions (persistent) | tdoa-engine |

### Apache Kafka (Event Streaming)

| Topic | Producers | Consumers | Purpose |
|-------|-----------|-----------|---------|
| `lmf.location.events` | location-request | event-manager | Location fix results |
| `lmf.subscriptions` | event-manager | event-manager | Subscription changes |

---

## 8. Key Algorithms

### A-GNSS Positioning (`gnss-engine`)

File: `services/gnss-engine/internal/positioning/gnss_solver.go`

1. **Kepler equation** — Newton-Raphson iteration, converges in 3-5 steps for GPS orbits
2. **Keplerian mechanics** — Full IS-GPS-200N orbit propagation with all 6 harmonic corrections
3. **Sagnac effect** — Earth rotation correction during signal flight time (~20ms for GPS)
4. **Relativistic clock** — Removes relativistic frequency shift from satellite clock
5. **WLS fix** — Weighted Least Squares with 4×4 Gaussian elimination; elevation-based weights
6. **Zhu's ECEF→Geodetic** — Closed-form conversion, no iteration required

### DL-TDOA Positioning (`tdoa-engine`)

File: `services/tdoa-engine/internal/positioning/tdoa_solver.go`

- **Chan's Algorithm** (1994) — Two-stage weighted least squares for TDOA
- Stage 1: linearises hyperbolic equations; Stage 2: uses stage 1 result to improve weights
- RSTD units: 30.72 MHz chip period (Ts = 32.55 ns); multiply by c = 299,792,458 m/s

### NR E-CID Positioning (`ecid-engine`)

File: `services/ecid-engine/internal/positioning/ecid_solver.go`

- **TA-based range**: `range = TA × 16 × Ts(SCS) × c/2`, uncertainty = range resolution
- **RSRP path loss centroid**: `d = d₀ × 10^((P_tx - PL₀ - RSRP)/(10n))`; weighted by inverse distance
- **Fusion**: inverse-variance weighted average of TA and RSRP estimates

### Multi-RTT Positioning (`rtt-engine`)

File: `services/rtt-engine/internal/positioning/rtt_solver.go`

- Range from UE+gNB Rx-Tx time difference: `r = (UeRxTx + GnbRxTx) × Ts/2 × c`
- WLS multilateration: iterative linearised least squares, ≤50 iterations, 1 cm convergence

### Position Fusion (`fusion-engine`)

Files: `services/fusion-engine/internal/fusion/`
- `weighted_fusion.go`: outlier rejection (3σ rule), inverse-variance weighted mean
- `kalman_filter.go`: 6-state EKF (position + velocity), constant velocity dynamics
- `fusion_engine.go`: combines both; single estimate goes directly to EKF

### Geofencing (`event-manager`)

File: `services/event-manager/internal/geofence/geofence_evaluator.go`

- **Ray casting** (Jordan curve theorem): count crossings of eastward ray with polygon edges
- **Haversine distance**: great-circle distance using spherical Earth model
- Area events: ENTER (outside→inside), LEAVE (inside→outside), WITHIN (stationary inside)

---

## 9. Common Package

Package path: `github.com/5g-lmf/common`

### Types (`common/types/`)

| File | Key Types |
|------|-----------|
| `lcs_types.go` | `LcsSession`, `LcsQoS`, `LocationEstimate`, `LocationRequest/Response`, `EventSubscription`, `LocationArea`, `LatLon`, `CellGeometry`, `PrsConfiguration` |
| `gnss_types.go` | `GnssConstellation`, `GnssEphemeris`, `KlobucharModel`, `GnssSignalMeasurement`, `GnssAssistanceData` |
| `positioning_types.go` | `PositionEstimate`, `RstdMeasurement`, `DlTdoaMeasurements`, `EcidMeasurements`, `MultiRttMeasurements`, `UeCapabilities`, `MethodSelectionRequest/Result` |

### Configuration (`common/config/`)

`config.go` defines `Config` struct with Viper-based loading:

```yaml
# Sections: server, grpc, redis, kafka, nrf, amf, udm, gnss,
#           tracing, metrics, log, cassandra, services
```

Key methods:
- `config.Load(path string) (*Config, error)` — load from YAML + env overrides
- `cfg.GRPC.ListenAddr() string` — returns `:port` for gRPC listening
- `cfg.GetCassandraHosts() []string` — returns Cassandra host list

### Middleware (`common/middleware/`)

| File | Exports |
|------|---------|
| `logging.go` | `NewLogger`, `WithSessionID`, `WithSupi`, `GrpcLoggingInterceptor`, `GrpcStreamLoggingInterceptor` |
| `metrics.go` | `LocationRequestsTotal`, `PositioningRequestsTotal`, `PositioningAccuracyMeters`, `ActiveSessions`, `PrivacyChecksTotal`, `StartMetricsServer(port int)` |

### Clients (`common/clients/`)

| File | Type | Key Methods |
|------|------|-------------|
| `redis_client.go` | `RedisClient` | `SetJSON`, `GetJSON`, `SetAdd`, `SetRemove`, `SetMembers`, key helpers |
| `kafka_client.go` | `KafkaProducer`, `KafkaConsumer` | `Publish(topic, key, value)`, `Subscribe(topic, handler)` |

---

## 10. Proto Definitions

```
proto/
├── lmf_common.proto      # PositioningMethod, LcsClientType, LcsQoS,
│                         # LocationEstimate, PositionEstimate, UeCapabilities
│
├── lmf_location.proto    # LocationRequestService: DetermineLocation, CancelLocation
│                         # SessionManagerService: Create/Get/Update/Delete
│                         # MethodSelectorService: SelectMethod
│                         # QosManagerService: EvaluateQoS
│
├── lmf_positioning.proto # GnssEngineService: GetAssistanceData, ComputePosition
│                         # TdoaEngineService: ComputePosition
│                         # EcidEngineService: ComputePosition
│                         # RttEngineService: ComputePosition
│                         # FusionEngineService: FusePositions
│
├── lmf_events.proto      # EventManagerService: Subscribe, Unsubscribe
│                         # PrivacyAuthService: CheckPrivacy, ValidateToken
│
└── lmf_protocol.proto    # ProtocolHandlerService: GetUECapabilities,
                          # TriggerMeasurement, GetTRPInformation
```

Generate Go code:
```bash
make proto
# Equivalent to:
protoc --go_out=. --go-grpc_out=. proto/*.proto
```

---

## 11. Security Architecture

```
External (Nllmf)                  Internal (gRPC mesh)
─────────────────                  ─────────────────────
TLS 1.3 (mTLS optional)           Istio mTLS (SPIFFE/SVID)
OAuth 2.0 Bearer token            No additional auth (mesh trust)
  → NRF-issued JWT
  → Scope: nlmf-loc
  → Validated by sbi-gateway
    AND privacy-auth

Privacy (TS 23.273 §8):
  CLASS_A: Emergency / Lawful Intercept → always allowed
  CLASS_B: Notify UE only → allowed immediately
  CLASS_C: Require UE consent within timeout (configurable, default 30s)
  CLASS_D: Blocked by subscriber profile → denied

Audit (TS 33.127):
  Every privacy decision written to Cassandra lcs_audit table
  Fields: record_id, timestamp, supi, session_id, lcs_client_type,
          privacy_class, outcome, denial_reason
```

---

## 12. Observability

### Prometheus Metrics (all on `/metrics`)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lmf_location_requests_total` | Counter | `outcome` | Total location requests |
| `lmf_positioning_requests_total` | Counter | `method`, `outcome` | Per-method counts |
| `lmf_positioning_accuracy_meters` | Histogram | `method` | Position accuracy distribution |
| `lmf_positioning_duration_seconds` | Histogram | `method` | End-to-end latency |
| `lmf_active_sessions` | Gauge | `type` | Live LCS sessions |
| `lmf_privacy_checks_total` | Counter | `outcome` | Privacy enforcement outcomes |
| `lmf_protocol_lpp_messages_total` | Counter | `type` | LPP message counts |
| `lmf_protocol_nrppa_messages_total` | Counter | `procedure` | NRPPa procedure counts |

### Logging

All services use **structured JSON logging** via `go.uber.org/zap`:

```json
{
  "level": "info",
  "ts": "2025-01-15T10:30:00.000Z",
  "caller": "server/location_server.go:28",
  "msg": "location request received",
  "supi": "imsi-001010000000001",
  "sessionId": "a3f2bc19-..."
}
```

### Distributed Tracing

OpenTelemetry traces exported to Jaeger (configured via `tracing.jaeger_endpoint`).

### Grafana Dashboards

Pre-built dashboard defined in `deploy/k8s/40-monitoring.yaml`:
- Location Requests/s
- P50/P95/P99 positioning latency
- Accuracy by method
- Active sessions
- Privacy check outcome pie chart

---

## 13. Configuration Reference

Each service loads `config/config.yaml` (overridable with env vars prefixed `LMF_`).

```yaml
# gRPC listen address
grpc:
  addr: ":9001"           # Or use port: 9001

# Downstream service addresses (location-request only)
services:
  sessionManager:  "session-manager:9002"
  methodSelector:  "method-selector:9004"
  protocolHandler: "protocol-handler:9003"
  gnssEngine:      "gnss-engine:9005"
  tdoaEngine:      "tdoa-engine:9006"
  ecidEngine:      "ecid-engine:9007"
  rttEngine:       "rtt-engine:9008"
  fusionEngine:    "fusion-engine:9009"
  qosManager:      "qos-manager:9013"
  privacyAuth:     "privacy-auth:9012"

# External 5GC
amf:
  baseUrl: "http://amf-service:8080"
udm:
  baseUrl: "http://udm-service:8080"

# Redis Cluster
redis:
  addresses: ["redis-cluster:6379"]
  password: ""

# Cassandra (privacy-auth)
cassandra:
  hosts: ["cassandra-0:9042", "cassandra-1:9042"]
  keyspace: "lmf_audit"

# Observability
metrics:
  port: 2111
log:
  level: "info"    # debug | info | warn | error
  format: "json"   # json | console
```

**Environment variable override examples:**
```bash
LMF_LOG_LEVEL=debug
LMF_GRPC_ADDR=:9001
LMF_REDIS_ADDRESSES=redis1:6379,redis2:6379
LMF_AMF_BASEURL=http://amf.5gc.svc:8080
JWT_SIGNING_KEY=your-production-key
```

---

## 14. File-by-File Reference

### common/

| File | Purpose |
|------|---------|
| `types/lcs_types.go` | All LCS domain types: session, QoS, events, areas, privacy classes |
| `types/gnss_types.go` | GNSS ephemeris, Klobuchar ionosphere, signal measurements |
| `types/positioning_types.go` | PositionEstimate, all measurement types, UE capabilities, method selection |
| `config/config.go` | Config struct, Viper loader, env var overrides, defaults |
| `middleware/logging.go` | Zap logger factory, gRPC logging interceptors, context helpers |
| `middleware/metrics.go` | All Prometheus metric registrations, StartMetricsServer |
| `clients/redis_client.go` | Redis Cluster client, JSON helpers, key conventions |
| `clients/kafka_client.go` | Kafka producer/consumer using IBM Sarama, topic constants |

### services/sbi-gateway/

| File | Purpose |
|------|---------|
| `main.go` | HTTP/2 server, Gin router, TLS config, graceful shutdown |
| `internal/handler/location_handler.go` | POST/DELETE `/nlmf-loc/v1/location-contexts` |
| `internal/handler/subscription_handler.go` | POST/DELETE `/nlmf-loc/v1/subscriptions` |
| `internal/handler/models.go` | JSON request/response structs |
| `internal/grpcclient/clients.go` | gRPC stubs for location-request + event-manager |
| `internal/middleware/auth.go` | Gin JWT Bearer validation middleware |

### services/location-request/

| File | Purpose |
|------|---------|
| `main.go` | Initialises all gRPC clients, wires orchestrator, serves |
| `internal/orchestrator/location_orchestrator.go` | **Core 9-step flow** (privacy→session→caps→select→trigger→compute→fuse→qos→return) |
| `internal/server/location_server.go` | gRPC DetermineLocation / CancelLocation handlers |
| `internal/grpc/clients.go` | gRPC connection bundle to all 10 downstream services |
| `internal/adapters/noop_adapters.go` | No-op stubs (replace with real gRPC adapters) |

### services/gnss-engine/

| File | Purpose |
|------|---------|
| `internal/positioning/gnss_solver.go` | **IS-GPS-200N Keplerian solver + WLS fix** |
| `internal/positioning/gnss_solver_test.go` | Tests: SV position, ECEF→geodetic, WLS convergence |
| `internal/assistance/data_provider.go` | Synthetic/real ephemeris provider + Klobuchar |
| `internal/assistance/cache.go` | Redis-backed assistance data cache |
| `internal/server/gnss_server.go` | gRPC GetAssistanceData + ComputePosition |

### services/tdoa-engine/

| File | Purpose |
|------|---------|
| `internal/positioning/tdoa_solver.go` | **Chan's two-stage WLS TDOA solver** |
| `internal/positioning/tdoa_solver_test.go` | Tests: SF Bay, Paris, equator scenarios |
| `internal/geometry/cell_geometry_store.go` | Two-level cache: in-memory + Redis |
| `internal/server/tdoa_server.go` | gRPC ComputePosition |

### services/ecid-engine/

| File | Purpose |
|------|---------|
| `internal/positioning/ecid_solver.go` | TA range + RSRP centroid + inverse-variance fusion |
| `internal/server/ecid_server.go` | gRPC ComputeEcid, metrics emission |

### services/rtt-engine/

| File | Purpose |
|------|---------|
| `internal/positioning/rtt_solver.go` | UE+gNB Rx-Tx range computation + WLS multilateration |
| `internal/server/rtt_server.go` | gRPC ComputeRtt, metrics emission |

### services/fusion-engine/

| File | Purpose |
|------|---------|
| `internal/fusion/weighted_fusion.go` | Outlier rejection + inverse-variance weighted mean |
| `internal/fusion/kalman_filter.go` | 6-state EKF: predict/update, per-SUPI tracking |
| `internal/fusion/fusion_engine.go` | Combines weighted fusion + EKF; quality index |
| `internal/server/fusion_server.go` | gRPC FusePositions |

### services/privacy-auth/

| File | Purpose |
|------|---------|
| `internal/privacy/privacy_checker.go` | **CLASS A/B/C/D enforcement**, UE consent waiting |
| `internal/auth/token_validator.go` | JWT Bearer token parse/validate, scope check |
| `internal/udm/udm_client.go` | HTTP Nudm_SDM_Get for subscriber privacy profile |
| `internal/audit/audit_store.go` | Cassandra writer for TS 33.127 audit records |
| `internal/server/privacy_server.go` | gRPC CheckPrivacy + ValidateToken |

### services/event-manager/

| File | Purpose |
|------|---------|
| `internal/geofence/geofence_evaluator.go` | Ray casting, Haversine, area/motion evaluation |
| `internal/geofence/geofence_evaluator_test.go` | 10 tests covering all geometric cases |
| `internal/subscription/subscription_store.go` | Redis subscription CRUD + SUPI index |
| `internal/notifier/notifier.go` | HTTP POST delivery of EventNotification JSON |
| `internal/scheduler/event_scheduler.go` | Periodic evaluation loop, per-SUPI last-position tracking |
| `internal/server/event_server.go` | gRPC Subscribe/Unsubscribe/GetSubscription |

### deploy/k8s/

| File | Resources |
|------|-----------|
| `00-namespace.yaml` | Namespace `lmf` with Istio injection |
| `01-configmap.yaml` | Shared env vars (Redis, Kafka, Cassandra, AMF, UDM endpoints) |
| `02-secrets.yaml` | JWT signing key, passwords |
| `10-16-*.yaml` | Deployment + Service + HPA for all 14 microservices |
| `20-22-*.yaml` | Redis/Kafka/Cassandra StatefulSets with PVCs |
| `30-network-policy.yaml` | Default deny + allow rules by service role |
| `40-monitoring.yaml` | Prometheus ServiceMonitor + Grafana dashboard ConfigMap |

---

*Document generated from source at `/Users/anusgupt/test-dir/lmf/`*
