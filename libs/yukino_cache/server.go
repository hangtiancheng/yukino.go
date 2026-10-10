package yukino_cache

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"log"

	pb "github.com/hangtiancheng/yukino.go/libs/yukino_cache/pb"
	client_v3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health "google.golang.org/grpc/health/grpc_health_v1"
)

type Server struct {
	pb.UnimplementedYukinoCacheServer
	addr         string
	svcName      string
	grpcServer   *grpc.Server
	healthServer *health.Server
	etcdCli      *client_v3.Client
	stopCh       chan error
	stopOnce     sync.Once
	opts         *ServerOptions
}

type ServerOptions struct {
	EtcdEndpoints []string
	DialTimeout   time.Duration
	MaxMsgSize    int
}

var DefaultServerOptions = &ServerOptions{
	EtcdEndpoints: []string{"localhost:2379"},
	DialTimeout:   5 * time.Second,
	MaxMsgSize:    4 << 20,
}

type ServerOption func(*ServerOptions)

func WithEtcdEndpoints(endpoints []string) ServerOption {
	return func(o *ServerOptions) {
		o.EtcdEndpoints = endpoints
	}
}

func WithDialTimeout(timeout time.Duration) ServerOption {
	return func(o *ServerOptions) {
		o.DialTimeout = timeout
	}
}

func NewServer(addr, svcName string, opts ...ServerOption) (*Server, error) {
	options := *DefaultServerOptions
	for _, opt := range opts {
		opt(&options)
	}

	etcdCli, err := client_v3.New(client_v3.Config{
		Endpoints:   options.EtcdEndpoints,
		DialTimeout: options.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %v", err)
	}

	serverOpts := []grpc.ServerOption{grpc.MaxRecvMsgSize(options.MaxMsgSize)}

	srv := &Server{
		addr:       addr,
		svcName:    svcName,
		grpcServer: grpc.NewServer(serverOpts...),
		etcdCli:    etcdCli,
		stopCh:     make(chan error),
		opts:       &options,
	}

	pb.RegisterYukinoCacheServer(srv.grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health.RegisterHealthServer(srv.grpcServer, healthServer)
	healthServer.SetServingStatus(svcName, grpc_health.HealthCheckResponse_SERVING)
	srv.healthServer = healthServer

	return srv, nil
}

func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	go func() {
		regCfg := &RegisterConfig{
			Endpoints:   s.opts.EtcdEndpoints,
			DialTimeout: s.opts.DialTimeout,
		}
		if err := registerWithConfig(s.svcName, s.addr, s.stopCh, regCfg); err != nil {
			log.Printf("failed to register service: %v", err)
		}
	}()

	log.Printf("Server starting at %s", s.addr)
	return s.grpcServer.Serve(lis)
}

func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		if s.healthServer != nil {
			s.healthServer.SetServingStatus(s.svcName, grpc_health.HealthCheckResponse_NOT_SERVING)
		}
		close(s.stopCh)
		s.grpcServer.GracefulStop()
		if s.etcdCli != nil {
			_ = s.etcdCli.Close()
		}
	})
}

func (s *Server) Get(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error) {
	group := GetGroup(req.Group)
	if group == nil {
		return nil, fmt.Errorf("group %s not found", req.Group)
	}
	group.stats.serverRequests.Add(1)

	view, err := group.Get(withPeerRequest(ctx), req.Key)
	if err != nil {
		return nil, err
	}
	return &pb.ResponseForGet{Value: view.ByteSlice()}, nil
}

func (s *Server) Set(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error) {
	group := GetGroup(req.Group)
	if group == nil {
		return nil, fmt.Errorf("group %s not found", req.Group)
	}

	if !isPeerRequest(ctx) {
		ctx = withPeerRequest(ctx)
	}
	if err := group.Set(ctx, req.Key, req.Value); err != nil {
		return nil, err
	}
	return &pb.ResponseForGet{Value: req.Value}, nil
}

func (s *Server) Delete(ctx context.Context, req *pb.Request) (*pb.ResponseForDelete, error) {
	group := GetGroup(req.Group)
	if group == nil {
		return nil, fmt.Errorf("group %s not found", req.Group)
	}

	err := group.Delete(withPeerRequest(ctx), req.Key)
	return &pb.ResponseForDelete{Value: err == nil}, err
}
