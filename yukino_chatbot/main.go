package main

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/ai"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/app"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/rpc_client"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/service"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/store"
	rpc "github.com/hangtiancheng/yukino.go/yukino_rpc/pkg/rpc"
)

type AIService struct {
	services *service.Services
}

func (s *AIService) Complete(ctx context.Context, req *rpc_client.AIRequest) (*rpc_client.AIResponse, error) {
	answer, result := s.services.Answer(ctx, req.Username, req.SessionID, req.Question, req.ModelType)
	return &rpc_client.AIResponse{Answer: answer, Code: int(result)}, nil
}

func (s *AIService) CompleteStream(req *rpc_client.AIRequest, stream rpc.ServerStream) error {
	return s.services.AnswerStream(stream.Context(), req.Username, req.SessionID, req.Question, req.ModelType, func(chunk string) {
		_ = stream.Send(&rpc_client.AIStreamChunk{Content: chunk})
	})
}

type rpcClient struct {
	conn *rpc.ClientConn
}

func (c *rpcClient) Complete(ctx context.Context, req rpc_client.AIRequest) (rpc_client.AIResponse, error) {
	var reply rpc_client.AIResponse
	err := c.conn.Invoke(ctx, "AIService", "Complete", &req, &reply)
	return reply, err
}

func (c *rpcClient) CompleteStream(ctx context.Context, req rpc_client.AIRequest) (rpc.ClientStream, error) {
	return c.conn.NewStream(ctx, "AIService", "CompleteStream", &req)
}

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	manager := ai.NewManager(cfg, st)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := st.LoadMessagesInto(ctx, manager); err != nil {
		log.Printf("load messages failed: %v", err)
	}
	services := service.New(cfg, st, manager)

	server := rpc.NewServer()
	server.Register("AIService", &AIService{services: services})
	lis, err := net.Listen("tcp", cfg.RPCAddr)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := server.Serve(lis); err != nil {
			log.Printf("rpc server stopped: %v", err)
		}
	}()

	cc, err := dialRPC(cfg.RPCAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer cc.Close()
	httpApp := app.New(cfg, services, newCache(), &rpcClient{conn: cc})
	addr := cfg.AppHost + ":" + cfg.AppPort
	log.Printf("server v2 listening on %s", addr)
	if err := httpApp.Engine().Listen(addr); err != nil {
		log.Fatal(err)
	}
}

func dialRPC(addr string) (*rpc.ClientConn, error) {
	var lastErr error
	for range 20 {
		cc, err := rpc.Dial(addr, rpc.WithTimeout(30*time.Second))
		if err == nil {
			return cc, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return nil, lastErr
}
