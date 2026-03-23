# Stage 1: Build
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /workspace

# Copy only what's needed
COPY common/ ./common/
COPY services/sbi-gateway/ ./services/sbi-gateway/

# Create a minimal go.work scoped to only this service
RUN go work init && \
    go work use ./common && \
    go work use ./services/sbi-gateway

WORKDIR /workspace/services/sbi-gateway

# Download dependencies
RUN go mod download

# Build binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" \
    -o /app/sbi-gateway ./main.go


# Stage 2: Runtime
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1000 lmf && \
    adduser -u 1000 -G lmf -D lmf

WORKDIR /app

COPY --from=builder /app/sbi-gateway .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /workspace/services/sbi-gateway/config/ ./config/

USER lmf

EXPOSE 443 8080 9090

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O- http://localhost:8080/health/live || exit 1

ENTRYPOINT ["/app/sbi-gateway"]