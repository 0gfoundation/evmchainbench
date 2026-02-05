package run

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	limiterpkg "github.com/0glabs/evmchainbench/lib/limiter"
)

type Transmitter struct {
	RpcUrl  string
	limiter *limiterpkg.RateLimiter
	client  *ethclient.Client
}

func NewTransmitter(rpcUrl string, limiter *limiterpkg.RateLimiter) (*Transmitter, error) {
	// Create custom HTTP client with high MaxIdleConnsPerHost to properly reuse connections
	// under high concurrency.
	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        2000,
			MaxIdleConnsPerHost: 2000,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 30 * time.Second,
	}

	rpcClient, err := rpc.DialHTTPWithClient(rpcUrl, httpClient)
	if err != nil {
		return nil, err
	}

	client := ethclient.NewClient(rpcClient)

	return &Transmitter{
		RpcUrl:  rpcUrl,
		limiter: limiter,
		client:  client,
	}, nil
}

func (t *Transmitter) Broadcast(txsMap map[int]types.Transactions) error {
	ch := make(chan error)

	for _, txs := range txsMap {
		go func(txs []*types.Transaction) {
			for _, tx := range txs {
				// Retry loop for each transaction with max retries
				const maxRetries = 50
				retryCount := 0
				for {
					if t.limiter == nil || t.limiter.AllowRequest() {
						err := broadcast(t.client, tx)
						if err != nil {
							// Check for transient errors to retry
							errMsg := err.Error()
							if strings.Contains(errMsg, "txpool is full") ||
								strings.Contains(errMsg, "Too Many Requests") ||
								strings.Contains(errMsg, "429") ||
								strings.Contains(errMsg, "dial tcp") || 
								strings.Contains(errMsg, "connection refused") ||
								strings.Contains(errMsg, "EOF") {
								
								retryCount++
								if retryCount >= maxRetries {
									// Skip this tx after max retries to avoid hanging
									break
								}
								// Backoff and retry
								time.Sleep(100 * time.Millisecond)
								continue
							}
							
							ch <- err
							return
						}
						break // Success, move to next tx
					} else {
						time.Sleep(10 * time.Millisecond)
					}
				}
			}
			ch <- nil
		}(txs)
	}

	senderCount := len(txsMap)
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
	return nil
}
