package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/privacy-auth/internal/audit"
	"github.com/5g-lmf/privacy-auth/internal/auth"
	"github.com/5g-lmf/privacy-auth/internal/privacy"
	"github.com/5g-lmf/privacy-auth/internal/server"
	"github.com/5g-lmf/privacy-auth/internal/udm"
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

	// UDM client for privacy profile lookup
	udmClient := udm.NewUDMHTTPClient(cfg.UDM.BaseURL, 5, logger)

	// Privacy checker with CLASS_C default
	checker := privacy.NewPrivacyChecker(udmClient, privacy.PrivacyClassC, 30, logger)

	// Token validator using signing key from config
	signingKey := []byte(os.Getenv("JWT_SIGNING_KEY"))
	if len(signingKey) == 0 {
		signingKey = []byte("dev-signing-key-replace-in-prod")
	}
	validator := auth.NewTokenValidator(signingKey, logger)

	// Audit store to Cassandra
	cassandraHosts := cfg.GetCassandraHosts()
	auditor, err := audit.NewAuditStore(cassandraHosts, "lmf_audit", logger)
	if err != nil {
		logger.Fatal("failed to connect to Cassandra", zap.Error(err))
	}
	defer auditor.Close()

	privacyServer := server.NewPrivacyAuthServer(checker, validator, auditor, logger)
	_ = privacyServer

	lis, err := net.Listen("tcp", cfg.GRPC.ListenAddr())
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	// pb.RegisterPrivacyAuthServiceServer(srv, privacyServer)
	reflection.Register(srv)

	logger.Info("Privacy Auth gRPC server starting", zap.String("addr", cfg.GRPC.ListenAddr()))

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down Privacy Auth")
	srv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ctx
}
