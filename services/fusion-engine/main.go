package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

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

	go middleware.StartMetricsServer(cfg.Metrics.Port)

	lis, err := net.Listen("tcp", cfg.GRPC.ListenAddr())
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	// pb.RegisterFusionEngineServiceServer(srv, server.NewFusionServer(logger))
	reflection.Register(srv)

	logger.Info("Fusion Engine gRPC server starting", zap.String("addr", cfg.GRPC.ListenAddr()))

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down Fusion Engine")
	srv.GracefulStop()

	_, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
}
