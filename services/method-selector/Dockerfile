FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /workspace

COPY common/ ./common/
COPY services/method-selector/ ./services/method-selector/

RUN go work init && \
    go work use ./common && \
    go work use ./services/method-selector

WORKDIR /workspace/services/method-selector

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" \
    -o /app/method-selector .

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1000 lmf && \
    adduser -u 1000 -G lmf -D lmf

WORKDIR /app

COPY --from=builder /app/method-selector .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

USER lmf

EXPOSE 8004 9004 2114

ENTRYPOINT ["/method-selector"]
