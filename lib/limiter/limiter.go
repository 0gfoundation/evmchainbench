package run

import (
	"fmt"
	"sync"
)

type RateLimiter struct {
	mutex     sync.Mutex
	remaining int
	drained   bool
}

func NewRateLimiter(maxRequests int) *RateLimiter {
	return &RateLimiter{
		remaining: maxRequests,
	}
}

func (rl *RateLimiter) AllowRequest() bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	if rl.drained {
		return true
	}
	if rl.remaining > 0 {
		rl.remaining--
		return true
	}
	return false
}

func (rl *RateLimiter) IncreaseLimit(newCapacity int) {
	rl.mutex.Lock()
	rl.remaining += newCapacity
	rl.mutex.Unlock()
	fmt.Println("Remaining requests:", rl.remaining)
}

// Drain disables rate limiting permanently. Called when the EthereumListener
// stops so that senders still spinning on AllowRequest can finish — without
// it, IncreaseLimit never gets called again and senders deadlock.
func (rl *RateLimiter) Drain() {
	rl.mutex.Lock()
	rl.drained = true
	rl.mutex.Unlock()
}
