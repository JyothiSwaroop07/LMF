package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/method-selector/internal/selector"
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

	methodSelector := selector.NewMethodSelector()
	_ = methodSelector // used by gRPC server (registered below)

	addr := fmt.Sprintf(":%d", cfg.GRPC.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Fatal("listening", zap.Error(err))
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	// grpc_pb.RegisterMethodSelectorServiceServer(grpcServer, server.New(methodSelector))

	go func() {
		logger.Info("method selector gRPC server starting", zap.String("addr", addr))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("grpc server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down method selector")
	grpcServer.GracefulStop()
}
