package run

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	limiterpkg "github.com/0glabs/evmchainbench/lib/limiter"
)

type Transmitter struct {
	// RpcUrls is a collection of RPC endpoints that the transmitter could use
	// to broadcast transactions. Senders are distributed across RPC endpoints
	// using round-robin fashion. For example, with 8 senders and 4 RPC endpoints,
	// each RPC endpoint will handle 2 senders.
	RpcUrls []string
	limiter *limiterpkg.RateLimiter
}

// NewTransmitter creates a new transmitter instance. It accepts one or many
// RPC endpoints. Passing an empty slice will result in an error.
func NewTransmitter(rpcUrls []string, limiter *limiterpkg.RateLimiter) (*Transmitter, error) {
	if len(rpcUrls) == 0 {
		return nil, fmt.Errorf("no rpc url provided")
	}

	return &Transmitter{
		RpcUrls: rpcUrls,
		limiter: limiter,
	}, nil
}

func (t *Transmitter) Broadcast(txsMap map[int]types.Transactions) error {
	// Validate: sender count should be a multiple of RPC count for even distribution
	senderCount := len(txsMap)
	rpcCount := len(t.RpcUrls)
	
	if senderCount == 0 {
		return fmt.Errorf("no transactions to broadcast")
	}
	
	if rpcCount == 0 {
		return fmt.Errorf("no rpc urls provided")
	}

	ch := make(chan error)

	// Iterate through sender indices sequentially to achieve stable mapping between
	// sender index and RPC endpoint. Ranging over a map returns keys in random
	// order, so using a deterministic loop avoids mismatches across runs.
	for senderIndex := 0; senderIndex < senderCount; senderIndex++ {
		txs, ok := txsMap[senderIndex]
		if !ok {
			ch <- fmt.Errorf("txsMap missing sender index %d", senderIndex)
			continue
		}

		// Select RPC endpoint using round-robin distribution
		// Each sender gets a dedicated RPC endpoint, but multiple senders can share the same RPC
		rpcUrl := t.RpcUrls[senderIndex%rpcCount]

		go func(rpcUrl string, txs []*types.Transaction) {
			client, err := ethclient.Dial(rpcUrl)
			if err != nil {
				ch <- err
				return
			}

			for _, tx := range txs {
				for {
					if t.limiter == nil || t.limiter.AllowRequest() {
						err := broadcast(client, tx)
						if err != nil {
							ch <- err
							return
						}
						break
					}

					time.Sleep(10 * time.Millisecond)
				}
			}

			ch <- nil
		}(rpcUrl, txs)
	}

	for i := 0; i < senderCount; i++ {
		err := <-ch
		if err != nil {
			return err
		}
	}

	return nil
}

func broadcast(client *ethclient.Client, tx *types.Transaction) error {
	err := client.SendTransaction(context.Background(), tx)
	if err != nil {
		return err
	}

	// Check tx hash
	// the hash can be abtained: tx.Hash().Hex()
	return nil
}
