package util

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ReceiptWaitTimeout returns the timeout used while polling for the receipts of a
// batch of recently-broadcast txs (used by the prepare* phases). Callers used to
// hard-code 20s, which is enough for single-bench setups against an idle chain
// but too tight when multiple benches launch concurrently against a multi-node
// chain: ~2000 funding txs per bench × N benches, all hitting different local
// mempools, with slow inter-mempool gossip + 3-node proposer rotation can take
// well over 20s before every receipt is observable from one node. Override via
// BENCH_RECEIPT_TIMEOUT_SECONDS (integer seconds). Default 120s.
func ReceiptWaitTimeout() time.Duration {
	if v := os.Getenv("BENCH_RECEIPT_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 120 * time.Second
}

func WaitForReceiptsOfTxs(client *ethclient.Client, txs types.Transactions, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, tx := range txs {
		txHash := tx.Hash()
		for {
			_, err := client.TransactionReceipt(context.Background(), txHash)
			if err == nil {
				break
			}

			if err != ethereum.NotFound {
				return err
			}

			select {
			case <-ctx.Done():
				return errors.New("Timeout before finding all receipts of txs")
			default:
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	return nil
}
