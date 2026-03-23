FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /workspace

COPY common/ ./common/
COPY services/assistance-data/ ./services/assistance-data/

RUN go work init && \
    go work use ./common && \
    go work use ./services/assistance-data

WORKDIR /workspace/services/assistance-data

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" \
    -o /app/assistance-data .

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1000 lmf && \
    adduser -u 1000 -G lmf -D lmf

WORKDIR /app

COPY --from=builder /app/assistance-data .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /workspace/services/assistance-data/config/ ./config/

USER lmf

EXPOSE 8010 9010 2120

ENTRYPOINT ["/app/assistance-data"]
