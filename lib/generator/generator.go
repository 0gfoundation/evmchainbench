package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/0glabs/evmchainbench/lib/account"
	"github.com/0glabs/evmchainbench/lib/contract_meta_data/erc20"
	"github.com/0glabs/evmchainbench/lib/store"
	"github.com/0glabs/evmchainbench/lib/util"
)

type Generator struct {
	FaucetAccount *account.Account
	Senders       []*account.Account
	Recipients    []string
	RpcUrl        string
	ChainID       *big.Int
	GasPrice      *big.Int
	ShouldPersist bool
	Store         *store.Store
	EIP1559       bool
}

func NewGenerator(rpcUrl, faucetPrivateKey string, senderCount, txCount int, shouldPersist bool, txStoreDir string) (*Generator, error) {
	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return &Generator{}, err
	}

	eip1559 := false
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return &Generator{}, err
	}
	if header.BaseFee != nil {
		eip1559 = true
	}

	fmt.Println("EIP-1559:", eip1559)

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		return &Generator{}, err
	}

	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return &Generator{}, err
	}

	faucetAccount, err := account.CreateFaucetAccount(client, faucetPrivateKey)
	if err != nil {
		return &Generator{}, err
	}

	senders := make([]*account.Account, senderCount)
	for i := 0; i < senderCount; i++ {
		s, err := account.NewAccount(client)
		if err != nil {
			return &Generator{}, err
		}
		senders[i] = s
	}

	recipients := make([]string, txCount)
	for i := 0; i < txCount; i++ {
		r, err := account.GenerateRandomAddress()
		if err != nil {
			return &Generator{}, err
		}
		recipients[i] = r
	}

	client.Close()

	return &Generator{
		FaucetAccount: faucetAccount,
		Senders:       senders,
		Recipients:    recipients,
		RpcUrl:        rpcUrl,
		ChainID:       chainID,
		GasPrice:      gasPrice,
		ShouldPersist: shouldPersist,
		Store:         store.NewStore(txStoreDir),
		EIP1559:       eip1559,
	}, nil
}

func (g *Generator) approveERC20(token common.Address, spender common.Address) {
	client, err := ethclient.Dial(g.RpcUrl)
	if err != nil {
		panic(err)
	}
	defer client.Close()
	txs := types.Transactions{}
	for _, sender := range append(g.Senders, g.FaucetAccount) {
		tx := GenerateContractCallingTx(
			sender.PrivateKey,
			token.Hex(),
			sender.GetNonce(),
			g.ChainID,
			g.GasPrice,
			erc20TransferGasLimit,
			erc20.MyTokenABI,
			"approve",
			spender,
			big.NewInt(1000000000000000000),
		)

		err = client.SendTransaction(context.Background(), tx)
		if err != nil {
			panic(err)
		}

		if g.ShouldPersist {
			g.Store.AddPrepareTx(tx)
		}

		txs = append(txs, tx)
	}

	err = util.WaitForReceiptsOfTxs(client, txs, 100*time.Second)
	if err != nil {
		panic(err)
	}
}

func (g *Generator) prepareERC20(contractAddressStr string) {
	client, err := ethclient.Dial(g.RpcUrl)
	if err != nil {
		panic(err)
	}
	defer client.Close()
	txs := types.Transactions{}
	for _, sender := range g.Senders {
		tx := GenerateContractCallingTx(
			g.FaucetAccount.PrivateKey,
			contractAddressStr,
			g.FaucetAccount.GetNonce(),
			g.ChainID,
			g.GasPrice,
			erc20TransferGasLimit,
			erc20.MyTokenABI,
			"transfer",
			sender.Address,
			big.NewInt(10000000),
		)

		err = client.SendTransaction(context.Background(), tx)
		if err != nil {
			panic(err)
		}

		if g.ShouldPersist {
			g.Store.AddPrepareTx(tx)
		}

		txs = append(txs, tx)
	}

	err = util.WaitForReceiptsOfTxs(client, txs, 100*time.Second)
	if err != nil {
		panic(err)
	}
}

func (g *Generator) prepareSenders() {
	client, err := ethclient.Dial(g.RpcUrl)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	value := new(big.Int)
	value.Mul(big.NewInt(1000000000000000000), big.NewInt(100)) // 100 Eth

	txs := types.Transactions{}

	for _, recipient := range g.Senders {
		signedTx, err := GenerateSimpleTransferTx(g.FaucetAccount.PrivateKey, recipient.Address.Hex(), g.FaucetAccount.GetNonce(), g.ChainID, g.GasPrice, value, g.EIP1559)
		if err != nil {
			panic(err)
		}

		err = client.SendTransaction(context.Background(), signedTx)
		if err != nil {
			panic(err)
		}

		if g.ShouldPersist {
			g.Store.AddPrepareTx(signedTx)
		}

		txs = append(txs, signedTx)
	}

	err = util.WaitForReceiptsOfTxs(client, txs, 100*time.Second)
	if err != nil {
		panic(err)
	}
}

func (g *Generator) estimateGas(msg ethereum.CallMsg) uint64 {
	client, err := ethclient.Dial(g.RpcUrl)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	gas, err := client.EstimateGas(context.Background(), msg)
	if err != nil {
		panic(err)
	}
	return gas
}

func (g *Generator) deployContract(gasLimit uint64, contractBin, contractABI string, args ...interface{}) (common.Address, error) {
	client, err := ethclient.Dial(g.RpcUrl)
	if err != nil {
		return common.Address{}, err
	}
	defer client.Close()
	tx, err := GenerateContractCreationTx(
		g.FaucetAccount.PrivateKey,
		g.FaucetAccount.GetNonce(),
		g.ChainID,
		g.GasPrice,
		gasLimit,
		contractBin,
		contractABI,
		args...,
	)
	if err != nil {
		panic(err)
	}

	err = client.SendTransaction(context.Background(), tx)
	if err != nil {
		panic(err)
	}

	ercContractAddress, err := bind.WaitDeployed(context.Background(), client, tx)
	if err != nil {
		fmt.Println("tx hash:", tx.Hash().Hex())
		panic(err)
	}

	if g.ShouldPersist {
		g.Store.AddPrepareTx(tx)
	}

	return ercContractAddress, nil
}

func (g *Generator) executeContractFunction(gasLimit uint64, contractAddress common.Address, contractABI, methodName string, args ...interface{}) {
	client, err := ethclient.Dial(g.RpcUrl)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	tx := GenerateContractCallingTx(
		g.FaucetAccount.PrivateKey,
		contractAddress.Hex(),
		g.FaucetAccount.GetNonce(),
		g.ChainID,
		g.GasPrice,
		gasLimit,
		contractABI,
		methodName,
		args...,
	)

	err = client.SendTransaction(context.Background(), tx)
	if err != nil {
		panic(err)
	}

	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		panic(err)
	}
	receiptJSON, err := json.MarshalIndent(receipt, "", "  ")

	if err != nil {
		panic(err)
	}
	if receipt.Status != 1 {
		panic(string(receiptJSON))
	}

	if g.ShouldPersist {
		g.Store.AddPrepareTx(tx)
	}
}

func (g *Generator) callContractView(contractAddress common.Address, contractABI, methodName string, args ...interface{}) []interface{} {
	client, err := ethclient.Dial(g.RpcUrl)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Parse the contract's ABI
	parsedABI, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		panic(err)
	}

	data, err := parsedABI.Pack(methodName, args...)
	if err != nil {
		panic(err)
	}

	// Create a call message
	msg := ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}

	// Send the call
	result, err := client.CallContract(context.Background(), msg, nil)
	if err != nil {
		panic(err)
	}

	unpacked, err := parsedABI.Unpack(methodName, result)
	if err != nil {
		panic(err)
	}

	return unpacked
}
