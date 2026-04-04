package middleware

import (
	"net"
	"strings"

	"secureshare/internal/config"

	"github.com/gin-gonic/gin"
)

const (
	// Context key for storing the client IP
	ClientIPKey = "client_ip"
)

type IPMiddleware struct {
	behindCloudflare bool
}

func NewIPMiddleware(cfg *config.Config) *IPMiddleware {
	return &IPMiddleware{
		behindCloudflare: cfg.BehindCloudflare,
	}
}

func (m *IPMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := m.getClientIP(c)
		c.Set(ClientIPKey, ip)
		c.Next()
	}
}

func (m *IPMiddleware) getClientIP(c *gin.Context) string {
	var ip string

	if m.behindCloudflare {
		// Cloudflare provides the real IP in CF-Connecting-IP header
		ip = c.GetHeader("CF-Connecting-IP")
		if ip != "" {
			return normalizeIP(ip)
		}

		// Fallback to X-Forwarded-For
		ip = c.GetHeader("X-Forwarded-For")
		if ip != "" {
			// X-Forwarded-For can contain multiple IPs, take the first one
			ips := strings.Split(ip, ",")
			if len(ips) > 0 {
				return normalizeIP(strings.TrimSpace(ips[0]))
			}
		}

		// Another fallback: X-Real-IP
		ip = c.GetHeader("X-Real-IP")
		if ip != "" {
			return normalizeIP(ip)
		}
	}

	// Default: use RemoteAddr
	ip = c.ClientIP()
	if ip == "" {
		ip = c.Request.RemoteAddr
	}

	return normalizeIP(ip)
}

// normalizeIP cleans up the IP address
func normalizeIP(ip string) string {
	// Remove port if present
	if strings.Contains(ip, ":") {
		host, _, err := net.SplitHostPort(ip)
		if err == nil {
			ip = host
		}
	}

	// Validate IP
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "unknown"
	}

	return parsed.String()
}

// GetClientIP is a helper function to get the client IP from context
func GetClientIP(c *gin.Context) string {
	ip, exists := c.Get(ClientIPKey)
	if !exists {
		return "unknown"
	}
	
	ipStr, ok := ip.(string)
	if !ok {
		return "unknown"
	}
	
	return ipStr
}

// IsPrivateIP checks if an IP is in a private range
func IsPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	// Check for private IP ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(parsed) {
			return true
		}
	}

	return false
}