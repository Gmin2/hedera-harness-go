// Package mock is an in process fake Hedera network. One in memory ledger is
// served through a fake consensus node (grpc), a fake mirror topic stream
// (grpc, same listener) and a fake mirror rest api (net/http).
//
// Transaction and query fees are always zero, so balances move by exactly the
// amounts a scenario transfers and balance assertions can be exact.
package mock

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"google.golang.org/grpc"
)

type Options struct {
	OperatorKey     *hiero.PrivateKey // nil = generate ed25519
	OperatorBalance int64             // tinybars, 0 = 1_000_000 hbar
	MirrorLag       time.Duration     // 0 = no lag, rest reads only see txs older than this
	Now             func() time.Time  // nil = time.Now, consensus timestamps must be strictly increasing
}

type Server struct {
	ledger      *ledger
	grpcServer  *grpc.Server
	grpcLis     net.Listener
	httpServer  *http.Server
	httpLis     net.Listener
	operatorKey hiero.PrivateKey
}

const (
	operatorAccount = 2
	nodeAccount     = 3
	feeAccount      = 98
	firstEntity     = 1001
)

func Start(opts Options) (*Server, error) {
	key := opts.OperatorKey
	if key == nil {
		k, err := hiero.PrivateKeyGenerateEd25519()
		if err != nil {
			return nil, fmt.Errorf("generate operator key: %w", err)
		}
		key = &k
	}
	balance := opts.OperatorBalance
	if balance == 0 {
		balance = 1_000_000 * 100_000_000
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	l := newLedger(now, opts.MirrorLag)
	l.genesis(protoKey(key.PublicKey()), balance)

	s := &Server{ledger: l, operatorKey: *key}

	var err error
	s.grpcLis, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s.httpLis, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.grpcLis.Close()
		return nil, err
	}

	s.grpcServer = newGRPCServer(l)
	s.httpServer = &http.Server{Handler: newMirrorREST(l), ReadHeaderTimeout: 5 * time.Second}

	go s.grpcServer.Serve(s.grpcLis)
	go s.httpServer.Serve(s.httpLis)
	return s, nil
}

func (s *Server) ConsensusAddr() string  { return s.grpcLis.Addr().String() }
func (s *Server) MirrorGRPCAddr() string { return s.grpcLis.Addr().String() }
func (s *Server) MirrorRESTURL() string  { return "http://" + s.httpLis.Addr().String() + "/api/v1" }

func (s *Server) Operator() (hiero.AccountID, hiero.PrivateKey) {
	return hiero.AccountID{Account: operatorAccount}, s.operatorKey
}

func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.httpServer.Shutdown(ctx)

	s.ledger.close()
	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		s.grpcServer.Stop()
	}
}
