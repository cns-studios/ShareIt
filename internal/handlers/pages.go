package handlers

import (
	"net/http"

	"secureshare/internal/config"

	"github.com/gin-gonic/gin"
)

type PageHandler struct {
	cfg *config.Config
}

func NewPageHandler(cfg *config.Config) *PageHandler {
	return &PageHandler{
		cfg: cfg,
	}
}

// Index handles GET /
func (h *PageHandler) Index(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"title":       "SecureShare - End-to-End Encrypted File Sharing",
		"baseURL":     h.cfg.BaseURL,
		"maxFileSize": h.cfg.MaxFileSize,
	})
}

// ToS handles GET /tos
func (h *PageHandler) ToS(c *gin.Context) {
	c.HTML(http.StatusOK, "tos.html", gin.H{
		"title":   "Terms of Service - SecureShare",
		"baseURL": h.cfg.BaseURL,
	})
}

// Privacy handles GET /privacy
func (h *PageHandler) Privacy(c *gin.Context) {
	c.HTML(http.StatusOK, "privacy.html", gin.H{
		"title":   "Privacy Policy - SecureShare",
		"baseURL": h.cfg.BaseURL,
	})
}

// SharedLookup handles GET /shared (lookup by numeric code)
func (h *PageHandler) SharedLookup(c *gin.Context) {
	c.HTML(http.StatusOK, "shared_lookup.html", gin.H{
		"title":   "Retrieve File - SecureShare",
		"baseURL": h.cfg.BaseURL,
	})
}

// SharedFile handles GET /shared/:id
func (h *PageHandler) SharedFile(c *gin.Context) {
	fileID := c.Param("id")
	
	c.HTML(http.StatusOK, "shared.html", gin.H{
		"title":   "Download File - SecureShare",
		"baseURL": h.cfg.BaseURL,
		"fileID":  fileID,
	})
}