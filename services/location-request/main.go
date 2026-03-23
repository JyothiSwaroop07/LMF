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
	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/location-request/internal/adapters"
	grpcclients "github.com/5g-lmf/location-request/internal/grpc"
	"github.com/5g-lmf/location-request/internal/orchestrator"
	"github.com/5g-lmf/location-request/internal/server"
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

	// Dial all downstream services
	clients, err := grpcclients.New(grpcclients.Config{
		SessionManagerAddr:  cfg.Services.SessionManager,
		MethodSelectorAddr:  cfg.Services.MethodSelector,
		ProtocolHandlerAddr: cfg.Services.ProtocolHandler,
		GnssEngineAddr:      cfg.Services.GnssEngine,
		TdoaEngineAddr:      cfg.Services.TdoaEngine,
		EcidEngineAddr:      cfg.Services.EcidEngine,
		RttEngineAddr:       cfg.Services.RttEngine,
		FusionEngineAddr:    cfg.Services.FusionEngine,
		QosManagerAddr:      cfg.Services.QosManager,
		PrivacyAuthAddr:     cfg.Services.PrivacyAuth,
	})
	if err != nil {
		logger.Fatal("failed to create gRPC clients", zap.Error(err))
	}
	defer clients.Close()

	// Build orchestrator with no-op adapter stubs (replace with real gRPC adapters)
	deps := orchestrator.Dependencies{
		Privacy:         &adapters.NoopPrivacy{},
		SessionMgr:      &adapters.NoopSession{},
		MethodSelector:  &adapters.NoopMethodSelector{},
		ProtocolHandler: &adapters.NoopProtocol{},
		GnssEngine:      &adapters.NoopEngine{},
		TdoaEngine:      &adapters.NoopEngine{},
		EcidEngine:      &adapters.NoopEcid{},
		RttEngine:       &adapters.NoopRtt{},
		FusionEngine:    &adapters.NoopFusion{},
		QosMgr:          &adapters.NoopQoS{},
	}
	_ = clients // gRPC connections used to build real adapters in production

	orch := orchestrator.NewLocationOrchestrator(deps, logger)
	locServer := server.NewLocationServer(orch, logger)
	_ = locServer

	lis, err := net.Listen("tcp", cfg.GRPC.ListenAddr())
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	pb.RegisterLocationRequestServiceServer(srv, locServer)
	//Swaroop uncommented the above like for registering the LocationServer with the gRPC server. This is necessary for the gRPC server to route incoming requests to the LocationServer's methods.

	reflection.Register(srv)

	logger.Info("Location Request gRPC server starting", zap.String("addr", cfg.GRPC.ListenAddr()))

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down Location Request")
	srv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = ctx
}
