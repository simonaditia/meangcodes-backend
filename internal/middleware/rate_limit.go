package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type IPRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

func NewIPRateLimiter(limit rate.Limit, burst int, ttl time.Duration) *IPRateLimiter {
	rl := &IPRateLimiter{
		visitors: make(map[string]*visitor),
		rate:     limit,
		burst:    burst,
		ttl:      ttl,
	}

	go rl.cleanupLoop()
	return rl
}

func (rl *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if ip == "" {
			ip = "unknown"
		}

		limiter := rl.getLimiter(ip)
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func (rl *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[ip]
	if !ok {
		limiter := rate.NewLimiter(rl.rate, rl.burst)
		rl.visitors[ip] = &visitor{limiter: limiter, lastSeen: now}
		return limiter
	}

	v.lastSeen = now
	return v.limiter
}

func (rl *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-rl.ttl)

		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}
