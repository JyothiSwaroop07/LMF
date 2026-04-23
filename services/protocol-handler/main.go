package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/5g-lmf/common/callbackregistry"
	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/protocol-handler/internal/lpp"
	namfcomm "github.com/5g-lmf/protocol-handler/internal/namfcomm"
	"github.com/5g-lmf/protocol-handler/internal/nrppa"
	"github.com/5g-lmf/protocol-handler/internal/server"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	logger, err := middleware.NewLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		panic("failed to create logger: " + err.Error())
	}
	defer logger.Sync()

	go middleware.StartMetricsServer(cfg.Metrics.Port)

	// ── Mobileum AMF address ──────────────────────────────────────────────────
	// dest_ip_address and dest_port from the DSX amf_1 node
	amfBaseURL := "http://192.168.145.26:80"

	// ── LMF callback base URL (reachable by Mobileum AMF) ────────────────────
	// sbi-gateway is exposed at 192.168.172.53:8000 via kind port mapping
	// AMF will POST LPP responses to:
	//   http://192.168.172.53:8000/namf-comm/callback/ue-contexts/<supi>/n1-n2-messages
	lmfCallbackBase := "http://192.168.172.53:8000/namf-comm/callback"
	lmfNfID := "a1b2c3d4-0000-0000-0000-lmf000000001"

	logger.Info("protocol-handler starting",
		zap.String("amfBaseURL", amfBaseURL),
		zap.String("lmfCallbackBase", lmfCallbackBase),
	)

	// ── Redis client (reuse existing common client) ───────────────────────────
	// redisClient, err := clients.NewRedisClient(cfg)
	redisAddrRaw := os.Getenv("LMF_REDIS_ADDRESSES")
	if redisAddrRaw == "" {
		redisAddrRaw = viper.GetString("redis.addresses")
	}
	if redisAddrRaw == "" {
		logger.Fatal("no redis addresses configured")
	}

	redisAddrs := strings.Split(redisAddrRaw, ",")
	logger.Info("connecting to redis cluster", zap.Strings("addrs", redisAddrs))

	redisClient := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs: redisAddrs,
		ClusterSlots: func(ctx context.Context) ([]redis.ClusterSlot, error) {
			slots := []redis.ClusterSlot{
				{
					Start: 0,
					End:   5460,
					Nodes: []redis.ClusterNode{
						{Addr: "redis-cluster-0.redis-cluster.lmf.svc.cluster.local:6379"},
					},
				},
				{
					Start: 5461,
					End:   10922,
					Nodes: []redis.ClusterNode{
						{Addr: "redis-cluster-1.redis-cluster.lmf.svc.cluster.local:6379"},
					},
				},
				{
					Start: 10923,
					End:   16383,
					Nodes: []redis.ClusterNode{
						{Addr: "redis-cluster-2.redis-cluster.lmf.svc.cluster.local:6379"},
					},
				},
			}
			return slots, nil
		},
		RouteByLatency: false,
		RouteRandomly:  false,
	})

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		logger.Warn("redis ping failed, continuing without persistent cache", zap.Error(err))
	}

	// ── Callback registry via Redis pub/sub ───────────────────────────────────
	// WaitForCallback() subscribes to "lmf:n1n2callback:<supi>"
	// Deliver() publishes to same channel (called from sbi-gateway)
	registry := callbackregistry.NewRegistryFromClient(redisClient, logger)

	// ── Namf_Communication client ─────────────────────────────────────────────
	namfClient := namfcomm.NewClient(amfBaseURL, lmfCallbackBase, lmfNfID, logger)

	// ── LPP handler with real AMF flow ────────────────────────────────────────
	// useRealLPP const in lpp.go controls real vs hardcoded mode
	lppHandler := lpp.NewLppHandler(amfBaseURL, namfClient, registry, logger)

	// ── NRPPa handler (unchanged) ─────────────────────────────────────────────
	nrppaHandler := nrppa.NewNrppaHandler(amfBaseURL, logger)

	protoServer := server.NewProtocolServer(lppHandler, nrppaHandler, logger)

	lis, err := net.Listen("tcp", cfg.GRPC.ListenAddr())
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	pb.RegisterProtocolHandlerServiceServer(srv, protoServer)
	reflection.Register(srv)

	logger.Info("Protocol Handler gRPC server starting", zap.String("addr", cfg.GRPC.ListenAddr()))

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down Protocol Handler")
	srv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ctx
}
