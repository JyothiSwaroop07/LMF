package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/5g-lmf/common/clients"
	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/event-manager/internal/notifier"
	"github.com/5g-lmf/event-manager/internal/server"
	"github.com/5g-lmf/event-manager/internal/subscription"
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

	store := subscription.NewSubscriptionStore(redisClient)
	notif := notifier.NewNotifier(logger)
	_ = notif // Used by scheduler; scheduler wired at runtime

	eventServer := server.NewEventServer(store, logger)
	_ = eventServer

	lis, err := net.Listen("tcp", cfg.GRPC.ListenAddr())
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	// pb.RegisterEventManagerServiceServer(srv, eventServer)
	reflection.Register(srv)

	logger.Info("Event Manager gRPC server starting", zap.String("addr", cfg.GRPC.ListenAddr()))

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down Event Manager")
	srv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ctx
}
