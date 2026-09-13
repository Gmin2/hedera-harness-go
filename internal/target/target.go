// Package target opens a network for a mode, starting a mock when needed.
package target

import (
	"context"
	"fmt"

	"github.com/Gmin2/hedera-harness-go/internal/mock"
	"github.com/Gmin2/hedera-harness-go/internal/network"
)

// Open connects to a network. For mock it starts a fresh in process
// hedera first, so every run begins from an empty ledger.
func Open(ctx context.Context, mode network.Mode) (*network.Target, func(), error) {
	if mode != network.Mock {
		cfg, err := network.FromEnv(mode)
		if err != nil {
			return nil, nil, err
		}
		t, err := network.Connect(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		return t, func() { t.Close() }, nil
	}

	srv, err := mock.Start(mock.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("start mock network: %w", err)
	}
	id, key := srv.Operator()
	t, err := network.Connect(ctx, network.Config{
		Mode:            network.Mock,
		Consensus:       srv.ConsensusAddr(),
		MirrorGRPC:      srv.MirrorGRPCAddr(),
		MirrorREST:      srv.MirrorRESTURL(),
		OperatorID:      id.String(),
		OperatorKey:     key.StringDer(),
		OperatorKeyType: "",
	})
	if err != nil {
		srv.Close()
		return nil, nil, err
	}
	return t, func() {
		t.Close()
		srv.Close()
	}, nil
}
