package run

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	limiterpkg "github.com/0glabs/evmchainbench/lib/limiter"
	"github.com/gorilla/websocket"
)

type BlockInfo struct {
	Time     int64
	TxCount  int64
	GasUsed  int64
	GasLimit int64
}

type EthereumListener struct {
	wsURL            string
	clAddress        string // New field for CL address
	conn             *websocket.Conn
	tmConn           *websocket.Conn // New field for Tendermint WebSocket connection
	limiter          *limiterpkg.RateLimiter
	blockStat        []BlockInfo
	quit             chan struct{}
	bestTPS          int64
	gasUsedAtBestTPS float64
	selfblocks       map[int]bool // Track blocks proposed by CL validator
	blockTxns        map[int]int  // Track transaction count for each block height
}

func NewEthereumListener(wsURL, clAddress string, limiter *limiterpkg.RateLimiter) *EthereumListener {
	return &EthereumListener{
		wsURL:      wsURL,
		clAddress:  clAddress,
		limiter:    limiter,
		quit:       make(chan struct{}),
		selfblocks: make(map[int]bool),
		blockTxns:  make(map[int]int),
	}
}

func (el *EthereumListener) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(el.wsURL, http.Header{})
	if err != nil {
		return fmt.Errorf("dial error: %v", err)
	}
	el.conn = conn
	return nil
}

// ConnectTendermint connects to the Tendermint WebSocket endpoint
func (el *EthereumListener) ConnectTendermint() error {
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:26657/websocket", http.Header{})
	if err != nil {
		return fmt.Errorf("tendermint dial error: %v", err)
	}
	el.tmConn = conn
	return nil
}

func (el *EthereumListener) SubscribeNewHeads() error {
	subscribeMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params":  []interface{}{"newHeads"},
	}
	err := el.conn.WriteJSON(subscribeMsg)
	if err != nil {
		return fmt.Errorf("subscribe error: %v", err)
	}

	go el.listenForMessages()

	return nil
}

// SubscribeTendermintNewBlock subscribes to Tendermint NewBlock events
func (el *EthereumListener) SubscribeTendermintNewBlock() error {
	subscribeMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "subscribe",
		"params":  []interface{}{"tm.event='NewBlock'"},
		"id":      1,
	}
	err := el.tmConn.WriteJSON(subscribeMsg)
	if err != nil {
		return fmt.Errorf("tendermint subscribe error: %v", err)
	}

	go el.listenForTendermintMessages()

	return nil
}

func (el *EthereumListener) listenForTendermintMessages() {
	for {
		_, message, err := el.tmConn.ReadMessage()
		if err != nil {
			log.Println("Tendermint WebSocket read error:", err)
			return
		}

		var response map[string]interface{}
		err = json.Unmarshal(message, &response)
		if err != nil {
			log.Println("Tendermint unmarshal error:", err)
			continue
		}

		el.handleTendermintMessage(response)
	}
}

func (el *EthereumListener) handleTendermintMessage(response map[string]interface{}) {
	// Get current time with millisecond precision
	currentTime := time.Now().Format("2006-01-02 15:04:05.000")

	// Check if this is a NewBlock event
	result, ok := response["result"]
	if !ok {
		return
	}

	if data, ok := result.(map[string]interface{})["data"]; ok {
		if dataMap, ok := data.(map[string]interface{}); ok {
			if value, ok := dataMap["value"]; ok {
				if valueMap, ok := value.(map[string]interface{}); ok {
					if block, ok := valueMap["block"]; ok {
						if blockMap, ok := block.(map[string]interface{}); ok {
							if header, ok := blockMap["header"]; ok {
								if headerMap, ok := header.(map[string]interface{}); ok {
									if height, ok := headerMap["height"]; ok {
										fmt.Printf("[%s] Tendermint NewBlock - Height: %v\n", currentTime, height)

										// Parse block height
										heightInt, _ := strconv.ParseInt(height.(string), 10, 64)

										// Check if proposer_address matches CL validator address
										if proposerAddress, ok := headerMap["proposer_address"]; ok {
											if proposerStr, ok := proposerAddress.(string); ok {
												if proposerStr == el.clAddress {
													// Check if this block height has transaction data and increase limit
													if txCount, exists := el.blockTxns[int(heightInt)]; exists {
														el.limiter.IncreaseLimit(txCount)
													}
													// Add block height to selfblocks set
													el.selfblocks[int(heightInt)] = true
													fmt.Printf("[%s] Self-proposed block detected - Height: %d\n", currentTime, heightInt)
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func (el *EthereumListener) listenForMessages() {
	for {
		_, message, err := el.conn.ReadMessage()
		if err != nil {
			return
		}

		var response map[string]interface{}
		err = json.Unmarshal(message, &response)
		if err != nil {
			log.Println("unmarshal error:", err)
			continue
		}

		if method, ok := response["method"]; ok && method == "eth_subscription" {
			el.handleNewHead(response)
		} else if id, ok := response["id"]; ok && id == float64(1) {
			// Check if this is a txpool_status response
			if result, ok := response["result"].(map[string]interface{}); ok {
				if _, hasPending := result["pending"]; hasPending {
					el.handleTxpoolStatus(response)
				} else {
					el.handleBlockResponse(response)
				}
			} else {
				el.handleBlockResponse(response)
			}
		} else {
			el.handleBlockResponse(response)
		}
	}
}

func (el *EthereumListener) handleNewHead(response map[string]interface{}) {
	params := response["params"].(map[string]interface{})
	result := params["result"].(map[string]interface{})
	blockNo := result["number"].(string)

	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_getBlockByNumber",
		"params":  []interface{}{blockNo, false},
	}
	err := el.conn.WriteJSON(request)
	if err != nil {
		log.Println("Failed to send block request:", err)
	}

	request = map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_getLogs",
		"params": []interface{}{
			map[string]interface{}{
				"fromBlock": blockNo,
				"toBlock":   blockNo,
			},
		},
	}
	err = el.conn.WriteJSON(request)
	if err != nil {
		log.Println("Failed to send log request:", err)
	}
}

func (el *EthereumListener) handleTxpoolStatus(response map[string]interface{}) {
	if result, ok := response["result"].(map[string]interface{}); ok {
		pending, _ := strconv.ParseInt(result["pending"].(string), 10, 64)
		queued, _ := strconv.ParseInt(result["queued"].(string), 10, 64)

		// Get current time with millisecond precision
		currentTime := time.Now().Format("2006-01-02 15:04:05.000")
		fmt.Printf("[%s] Mempool Status - Pending: %d, Queued: %d\n", currentTime, pending, queued)
	}
}

func (el *EthereumListener) handleBlockResponse(response map[string]interface{}) {
	if result, ok := response["result"].(map[string]interface{}); ok {
		if txns, ok := result["transactions"].([]interface{}); ok {
			systs := time.Now().Unix()
			ts := systs
			// fmt.Println("ts", ts, "systs", systs)
			gasUsed, _ := strconv.ParseInt(result["gasUsed"].(string)[2:], 16, 64)
			gasLimit, _ := strconv.ParseInt(result["gasLimit"].(string)[2:], 16, 64)

			// Get current time with millisecond precision
			currentTime := time.Now().Format("2006-01-02 15:04:05.000")
			// Get block number
			blockNumber := result["number"].(string)

			// Record transaction count for this block height
			blockHeight, _ := strconv.ParseInt(blockNumber[2:], 16, 64) // Remove "0x" prefix and parse as hex

			// Only increase limit if this block is in selfblocks (proposed by CL validator)
			if el.selfblocks[int(blockHeight)] {
				el.limiter.IncreaseLimit(len(txns))
			}

			el.blockTxns[int(blockHeight)] = len(txns)

			// Output current time, block number, and transaction count
			fmt.Printf("[%s] Block: %s, Transactions: %d, GasUsed: %d, GasLimit: %d\n", currentTime, blockNumber, len(txns), gasUsed, gasLimit)

			el.blockStat = append(el.blockStat, BlockInfo{
				Time:     ts,
				TxCount:  int64(len(txns)),
				GasUsed:  gasUsed,
				GasLimit: gasLimit,
			})
			// keep only the last 60 seconds of blocks
			for {
				if len(el.blockStat) == 1 {
					break
				}
				if el.blockStat[len(el.blockStat)-1].Time-el.blockStat[0].Time > 60 {
					el.blockStat = el.blockStat[1:]
				} else {
					break
				}
			}
			timeSpan := el.blockStat[len(el.blockStat)-1].Time - el.blockStat[0].Time
			// fmt.Println("timeSpan", timeSpan)
			// calculate TPS and gas used percentage
			if timeSpan > 10 {
				totalTxCount := int64(0)
				totalGasLimit := int64(0)
				totalGasUsed := int64(0)
				for _, block := range el.blockStat {
					totalTxCount += block.TxCount
					totalGasLimit += block.GasLimit
					totalGasUsed += block.GasUsed
				}
				tps := totalTxCount / timeSpan
				gasUsedPercent := float64(totalGasUsed) / float64(totalGasLimit)
				if tps > el.bestTPS {
					el.bestTPS = tps
					el.gasUsedAtBestTPS = gasUsedPercent
				}
				fmt.Printf("TPS: %d GasUsed%%: %.2f%%\n", tps, gasUsedPercent*100)
				if totalTxCount < 100 {
					// exit if total tx count is less than 100
					fmt.Printf("Best TPS: %d GasUsed%%: %.2f%%\n", el.bestTPS, el.gasUsedAtBestTPS*100)
					el.Close()
					return
				}

				// to avoid waiting 50 seconds after the transmission is complete
				if len(el.blockStat) >= 3 {
					for i := 1; i <= 3; i++ {
						if el.blockStat[len(el.blockStat)-i].TxCount != 0 {
							return
						}
					}
					fmt.Printf("Best TPS: %d GasUsed%%: %.2f%%\n", el.bestTPS, el.gasUsedAtBestTPS*100)
					el.Close()
				}
			}
		}
	} else {
		if result, ok := response["result"].([]interface{}); ok {
			if len(result) > 0 {
				fmt.Println("Logs:", len(result))
			}
		}
	}
}

func (el *EthereumListener) Close() {
	if el.conn != nil {
		el.conn.Close()
	}
	if el.tmConn != nil {
		el.tmConn.Close()
	}
	close(el.quit)
}
