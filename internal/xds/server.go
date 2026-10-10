// Package xds serves Envoy its configuration. The Gateway controllers translate
// each Gateway into a snapshot, and the Envoy proxy provisioned for that Gateway
// streams it over ADS.
package xds

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	xdslog "github.com/envoyproxy/go-control-plane/pkg/log"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
)

// DefaultPort is the port the xDS server listens on, and the one the Service in
// front of the manager forwards.
const DefaultPort = 18000

// Keepalives let a replica notice an Envoy that went away without closing its
// stream, and keep idle streams open through connection-tracking middleboxes.
const (
	keepaliveTime    = 30 * time.Second
	keepaliveMinTime = 15 * time.Second
)

// NewCache returns the snapshot cache the controllers write to and the server
// reads from. Snapshots are keyed by Envoy node id, which is NodeID of the
// Gateway the proxy serves.
func NewCache(log logr.Logger) cachev3.SnapshotCache {
	return cachev3.NewSnapshotCache(true, cachev3.IDHash{}, logger(log))
}

// Server is the xDS gRPC server, run by the manager.
type Server struct {
	// Address is the address to listen on.
	Address string

	// Cache holds the snapshot for every Gateway.
	Cache cachev3.SnapshotCache

	Log logr.Logger
}

// NeedLeaderElection reports false: every replica serves xDS, so an Envoy that
// reaches any of them through the Service gets its configuration.
func (s *Server) NeedLeaderElection() bool {
	return false
}

// Start serves until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.Address)
	if err != nil {
		return fmt.Errorf("listening for xDS on %s: %w", s.Address, err)
	}

	srv := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time: keepaliveTime,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             keepaliveMinTime,
			PermitWithoutStream: true,
		}),
	)
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(srv, serverv3.NewServer(ctx, s.Cache, nil))

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	s.Log.Info("Serving xDS", "address", lis.Addr().String())
	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("serving xDS: %w", err)
	}

	return nil
}

// logger adapts a logr.Logger to the logging interface go-control-plane takes.
// Its debug output reports every request and response, so it goes to V(2).
func logger(log logr.Logger) xdslog.Logger {
	return xdslog.LoggerFuncs{
		DebugFunc: func(format string, args ...any) { log.V(2).Info(fmt.Sprintf(format, args...)) },
		InfoFunc:  func(format string, args ...any) { log.V(1).Info(fmt.Sprintf(format, args...)) },
		WarnFunc:  func(format string, args ...any) { log.Info(fmt.Sprintf(format, args...)) },
		ErrorFunc: func(format string, args ...any) { log.Error(nil, fmt.Sprintf(format, args...)) },
	}
}
