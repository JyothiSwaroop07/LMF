package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/5g-lmf/assistance-data/internal/provider"
	"github.com/5g-lmf/assistance-data/internal/server"
	"github.com/5g-lmf/assistance-data/internal/store"
	"github.com/5g-lmf/common/clients"
	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
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

	redisClient, err := clients.NewRedisClient(cfg)
	if err != nil {
		logger.Fatal("failed to connect to Redis", zap.Error(err))
	}

	go middleware.StartMetricsServer(cfg.Metrics.Port)

	assistStore := store.NewAssistanceStore(redisClient)
	gnssProvider := provider.NewGNSSProvider(logger)
	assistServer := server.NewAssistanceServer(assistStore, gnssProvider, logger)
	_ = assistServer

	lis, err := net.Listen("tcp", cfg.GRPC.ListenAddr())
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	// pb.RegisterAssistanceDataServiceServer(srv, assistServer)
	reflection.Register(srv)

	logger.Info("Assistance Data gRPC server starting", zap.String("addr", cfg.GRPC.ListenAddr()))

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down Assistance Data")
	srv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ctx
}
