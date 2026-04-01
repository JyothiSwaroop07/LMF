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
	"github.com/5g-lmf/protocol-handler/internal/lpp"
	"github.com/5g-lmf/protocol-handler/internal/nrppa"
	"github.com/5g-lmf/protocol-handler/internal/server"
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

	amfURL := "http://192.168.138.23:7779" // Update to point to your AMF service

	logger.Info("AMF URL", zap.String("amfURL", amfURL))

	lppHandler := lpp.NewLppHandler(amfURL, logger)
	nrppaHandler := nrppa.NewNrppaHandler(amfURL, logger)
	protoServer := server.NewProtocolServer(lppHandler, nrppaHandler, logger)
	_ = protoServer

	lis, err := net.Listen("tcp", cfg.GRPC.ListenAddr())
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(middleware.GrpcLoggingInterceptor(logger)),
	)

	//uncomment the line below and implement the ProtocolHandlerServiceServer interface in server.ProtocolServer to enable gRPC handling of protocol messages.
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
