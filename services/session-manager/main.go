package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/5g-lmf/common/clients"
	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/session-manager/internal/server"
	"github.com/5g-lmf/session-manager/internal/store"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading config: %v\n", err)
		os.Exit(1)
	}

	logger, err := middleware.NewLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	middleware.StartMetricsServer(cfg.Metrics.Port)

	// Connect to Redis
	redisClient, err := clients.NewRedisClient(cfg)
	if err != nil {
		logger.Fatal("connecting to redis", zap.Error(err))
	}
	defer redisClient.Close()

	logger.Info("connected to redis")

	// Create session store
	sessionStore := store.NewSessionStore(redisClient)

	// Start gRPC server
	addr := fmt.Sprintf(":%d", cfg.GRPC.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Fatal("listening", zap.Error(err))
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	sessionServer := server.NewSessionServer(sessionStore, logger)
	sessionServer.Register(grpcServer)

	go func() {
		logger.Info("session manager gRPC server starting", zap.String("addr", addr))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("grpc server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down session manager")
	grpcServer.GracefulStop()
	logger.Info("session manager stopped")
}
