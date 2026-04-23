package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/5g-lmf/common/pb"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/5g-lmf/gnss-engine/internal/assistance"
	"github.com/5g-lmf/gnss-engine/internal/positioning"
	"github.com/5g-lmf/gnss-engine/internal/server"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	viper.SetConfigFile("config/config.yaml")
	if err := viper.ReadInConfig(); err != nil {
		logger.Fatal("failed to read config", zap.Error(err))
	}

	// redisAddrs := viper.GetStringSlice("redis.addresses")

	redisAddrRaw := os.Getenv("LMF_REDIS_ADDRESSES")
	if redisAddrRaw == "" {
		redisAddrRaw = viper.GetString("redis.addresses")
	}
	if redisAddrRaw == "" {
		logger.Fatal("no redis addresses configured")
	}

	redisAddrs := strings.Split(redisAddrRaw, ",")
	logger.Info("connecting to redis cluster", zap.Strings("addrs", redisAddrs))

	// rdb := redis.NewClusterClient(&redis.ClusterOptions{
	// 	Addrs: redisAddrs,
	// })

	rdb := redis.NewClusterClient(&redis.ClusterOptions{
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
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		logger.Warn("redis ping failed, continuing without persistent cache", zap.Error(err))
	}

	cache := assistance.NewAssistanceDataCache(rdb, logger)
	provider := assistance.NewAssistanceDataProvider(
		cache,
		viper.GetString("gnss_server_url"),
		logger,
	)
	solver := positioning.NewGnssSolver(logger)
	gnssServer := server.NewGnssServer(provider, solver, logger)

	// Proactive assistance data refresh goroutine.
	refreshInterval := time.Duration(viper.GetInt("assistance_refresh_interval_seconds")) * time.Second
	if refreshInterval <= 0 {
		refreshInterval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 15*time.Second)
			if err := provider.RefreshAll(refreshCtx); err != nil {
				logger.Warn("assistance data refresh failed", zap.Error(err))
			}
			refreshCancel()
		}
	}()

	// Metrics HTTP server.
	metricsPort := viper.GetInt("metrics.port")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
		})
		addr := fmt.Sprintf(":%d", metricsPort)
		logger.Info("starting metrics server", zap.String("addr", addr))
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Error("metrics server exited", zap.Error(err))
		}
	}()

	// gRPC server.
	grpcPort := viper.GetInt("grpc.port")
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		logger.Fatal("failed to listen", zap.Int("port", grpcPort), zap.Error(err))
	}

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingInterceptor(logger),
		),
	)
	// server.RegisterGnssEngineServiceServer(grpcSrv, gnssServer)
	pb.RegisterGnssEngineServiceServer(grpcSrv, gnssServer)
	reflection.Register(grpcSrv)

	go func() {
		logger.Info("starting gRPC server", zap.Int("port", grpcPort))
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	grpcSrv.GracefulStop()
	logger.Info("server stopped")
}

// loggingInterceptor logs every unary RPC call with duration.
func loggingInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.Info("rpc",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", time.Since(start)),
			zap.Error(err),
		)
		return resp, err
	}
}
