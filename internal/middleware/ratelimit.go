package middleware

import (
	"net/http"

	"secureshare/internal/models"
	"secureshare/internal/storage"

	"github.com/gin-gonic/gin"
)

type RateLimiter struct {
	redis *storage.Redis
}

func NewRateLimiter(redis *storage.Redis) *RateLimiter {
	return &RateLimiter{
		redis: redis,
	}
}

func (rl *RateLimiter) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := GetClientIP(c)
		
		// Skip rate limiting for unknown IPs (shouldn't happen, but safety first)
		if ip == "unknown" {
			c.Next()
			return
		}

		// Check rate limit
		allowed, err := rl.redis.CheckRateLimit(c.Request.Context(), ip)
		if err != nil {
			// On Redis error, allow the request but log
			// In production, you might want to be more strict
			c.Next()
			return
		}

		if !allowed {
			c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
				Error: models.ErrRateLimited.Message,
				Code:  models.ErrRateLimited.Code,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// NewStrictRateLimiter creates a rate limiter that blocks on Redis errors
func NewStrictRateLimiter(redis *storage.Redis) *RateLimiter {
	return &RateLimiter{
		redis: redis,
	}
}

func (rl *RateLimiter) StrictHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := GetClientIP(c)
		
		if ip == "unknown" {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error: "Could not determine client IP",
				Code:  "UNKNOWN_IP",
			})
			c.Abort()
			return
		}

		allowed, err := rl.redis.CheckRateLimit(c.Request.Context(), ip)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
				Error: "Service temporarily unavailable",
				Code:  "SERVICE_ERROR",
			})
			c.Abort()
			return
		}

		if !allowed {
			c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
				Error: models.ErrRateLimited.Message,
				Code:  models.ErrRateLimited.Code,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// DownloadRateLimiter is a separate rate limiter for downloads
// with different limits
type DownloadRateLimiter struct {
	redis      *storage.Redis
	maxPerMin  int64
	keyPrefix  string
}

func NewDownloadRateLimiter(redis *storage.Redis, maxPerMinute int64) *DownloadRateLimiter {
	return &DownloadRateLimiter{
		redis:     redis,
		maxPerMin: maxPerMinute,
		keyPrefix: "ratelimit:download:",
	}
}

func (drl *DownloadRateLimiter) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := GetClientIP(c)
		
		if ip == "unknown" {
			c.Next()
			return
		}

		// Use custom rate limiting for downloads
		ctx := c.Request.Context()
		key := drl.keyPrefix + ip
		
		count, err := drl.redis.Client().Incr(ctx, key).Result()
		if err != nil {
			c.Next()
			return
		}

		// Set expiry on first request
		if count == 1 {
			drl.redis.Client().Expire(ctx, key, 60) // 1 minute
		}

		if count > drl.maxPerMin {
			c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
				Error: "Too many download requests, please slow down",
				Code:  "DOWNLOAD_RATE_LIMITED",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}