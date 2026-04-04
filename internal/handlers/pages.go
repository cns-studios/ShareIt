package handlers

import (
	"net/http"

	"shareit/internal/config"

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

 
func (h *PageHandler) Index(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"title":       "ShareIt - End-to-End Encrypted File Sharing",
		"baseURL":     h.cfg.BaseURL,
		"maxFileSize": h.cfg.MaxFileSize,
	})
}

 
func (h *PageHandler) ToS(c *gin.Context) {
	c.HTML(http.StatusOK, "tos.html", gin.H{
		"title":   "Terms of Service - ShareIt",
		"baseURL": h.cfg.BaseURL,
	})
}

 
func (h *PageHandler) Privacy(c *gin.Context) {
	c.HTML(http.StatusOK, "privacy.html", gin.H{
		"title":   "Privacy Policy - ShareIt",
		"baseURL": h.cfg.BaseURL,
	})
}

 
func (h *PageHandler) SharedLookup(c *gin.Context) {
	c.HTML(http.StatusOK, "shared_lookup.html", gin.H{
		"title":   "Retrieve File - ShareIt",
		"baseURL": h.cfg.BaseURL,
	})
}

 
func (h *PageHandler) SharedFile(c *gin.Context) {
	fileID := c.Param("id")
	
	c.HTML(http.StatusOK, "shared.html", gin.H{
		"title":   "Download File - ShareIt",
		"baseURL": h.cfg.BaseURL,
		"fileID":  fileID,
	})
}