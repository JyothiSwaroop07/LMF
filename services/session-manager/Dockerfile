FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /workspace

COPY common/ ./common/
COPY services/session-manager/ ./services/session-manager/

RUN go work init && \
    go work use ./common && \
    go work use ./services/session-manager

WORKDIR /workspace/services/session-manager

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" \
    -o /app/session-manager .

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1000 lmf && \
    adduser -u 1000 -G lmf -D lmf

WORKDIR /app

COPY --from=builder /app/session-manager .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /workspace/services/session-manager/config/ ./config/

USER lmf

EXPOSE 8002 9002 2112

ENTRYPOINT ["/app/session-manager"]
