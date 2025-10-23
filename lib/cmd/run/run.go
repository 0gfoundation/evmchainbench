package run

import (
	"log"
	"os"
	"strings"

	"github.com/0glabs/evmchainbench/lib/generator"
	limiterpkg "github.com/0glabs/evmchainbench/lib/limiter"
	"github.com/ethereum/go-ethereum/core/types"
)

func Run(httpRpc, rpcFile, wsRpc, faucetPrivateKey, clAddress string, senderCount, txCount int, txType string, mempool int) {
	var rpcUrls []string

	if rpcFile != "" {
		// Read RPC URLs from file (starting from line 2)
		data, err := os.ReadFile(rpcFile)
		if err != nil {
			log.Fatalf("failed to read rpc file: %v", err)
		}

		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Extract IP from lines like "user@ip" or "ip"
			var ip string
			if strings.Contains(line, "@") {
				// Extract IP after @ symbol
				parts := strings.Split(line, "@")
				if len(parts) == 2 {
					ip = strings.TrimSpace(parts[1])
				} else {
					log.Printf("warning: invalid format in line %d: %s", i+1, line)
					continue
				}
			} else {
				// Assume the line is already an IP
				ip = line
			}

			// Convert IP to full RPC URL
			rpcUrl := "http://" + ip + ":8545"
			rpcUrls = append(rpcUrls, rpcUrl)
		}
	} else {
		rpcUrls = strings.Split(httpRpc, ",")
	}

	if len(rpcUrls) == 0 {
		log.Fatalf("no rpc urls provided: either specify --http-rpc or --rpc-file")
	}

	/*if len(rpcUrls) < senderCount {
		log.Fatalf("insufficient rpc urls: have %d, need at least %d", len(rpcUrls), senderCount)
	}*/

	primaryRpcUrl := rpcUrls[0]

	generator, err := generator.NewGenerator(primaryRpcUrl, faucetPrivateKey, senderCount, txCount, false, "")
	if err != nil {
		log.Fatalf("Failed to create generator: %v", err)
	}

	var txsMap map[int]types.Transactions

	switch txType {
	case "simple":
		txsMap, err = generator.GenerateSimple()
	case "erc20":
		txsMap, err = generator.GenerateERC20()
	case "uniswap":
		txsMap, err = generator.GenerateUniswap()
	default:
		log.Fatalf("Transaction type \"%v\" is not valid", txType)
	}
	if err != nil {
		log.Fatalf("Failed to generate transactions: %v", err)
	}

	limiter := limiterpkg.NewRateLimiter(mempool)

	ethListener := NewEthereumListener(wsRpc, clAddress, limiter)
	err = ethListener.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket: %v", err)
	}

	// Connect to Tendermint WebSocket
	err = ethListener.ConnectTendermint()
	if err != nil {
		log.Fatalf("Failed to connect to Tendermint WebSocket: %v", err)
	}

	// Subscribe new heads
	err = ethListener.SubscribeNewHeads()
	if err != nil {
		log.Fatalf("Failed to subscribe to new heads: %v", err)
	}

	// Subscribe to Tendermint NewBlock events
	err = ethListener.SubscribeTendermintNewBlock()
	if err != nil {
		log.Fatalf("Failed to subscribe to Tendermint NewBlock events: %v", err)
	}

	transmitter, err := NewTransmitter(rpcUrls, limiter)
	if err != nil {
		log.Fatalf("Failed to create transmitter: %v", err)
	}

	err = transmitter.Broadcast(txsMap)
	if err != nil {
		log.Fatalf("Failed to broadcast transactions: %v", err)
	}

	<-ethListener.quit
}
