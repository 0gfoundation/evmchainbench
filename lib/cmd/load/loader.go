package load

import (
	"context"
	"time"

	"github.com/0glabs/evmchainbench/lib/cmd/run"
	"github.com/0glabs/evmchainbench/lib/store"
	"github.com/0glabs/evmchainbench/lib/util"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Loader struct {
	RpcUrl string
	Store  *store.Store
}

func NewLoader(rpcUrl, txStoreDir string) *Loader {
	return &Loader{
		RpcUrl: rpcUrl,
		Store:  store.NewStore(txStoreDir),
	}
}

func (l *Loader) LoadAndRun() error {
	client, err := ethclient.Dial(l.RpcUrl)
	if err != nil {
		return err
	}
	defer client.Close()

	txs, err := l.Store.LoadPrepareTxs()
	if err != nil {
		return err
	}

	for _, tx := range txs {
		err = client.SendTransaction(context.Background(), tx)
		if err != nil {
			return err
		}
	}

	err = util.WaitForReceiptsOfTxs(client, txs, 100*time.Second)
	if err != nil {
		return err
	}

	txsMap, err := l.Store.LoadTxsMap()
	if err != nil {
		return err
	}

	// TODO, add limiter
	transmitter, err := run.NewTransmitter([]string{l.RpcUrl}, nil)
	if err != nil {
		return err
	}

	err = transmitter.Broadcast(txsMap)
	if err != nil {
		return err
	}

	return nil
}
