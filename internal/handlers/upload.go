package handlers

import (
	"fmt"
	"net/http"

	"secureshare/internal/config"
	"secureshare/internal/middleware"
	"secureshare/internal/models"
	"secureshare/internal/services"
	"secureshare/internal/storage"

	"github.com/gin-gonic/gin"
)

type UploadHandler struct {
	cfg           *config.Config
	db            *storage.Postgres
	redis         *storage.Redis
	fs            *storage.Filesystem
	uploadService *services.Upload
}

func NewUploadHandler(
	cfg *config.Config,
	db *storage.Postgres,
	redis *storage.Redis,
	fs *storage.Filesystem,
	uploadService *services.Upload,
) *UploadHandler {
	return &UploadHandler{
		cfg:           cfg,
		db:            db,
		redis:         redis,
		fs:            fs,
		uploadService: uploadService,
	}
}

// Init handles POST /api/upload/init
func (h *UploadHandler) Init(c *gin.Context) {
	var req models.UploadInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "Invalid request body",
			Code:    "INVALID_REQUEST",
			Details: err.Error(),
		})
		return
	}

	// Validate file size
	if req.FileSize > h.cfg.MaxFileSize {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: models.ErrFileTooLarge.Message,
			Code:  models.ErrFileTooLarge.Code,
		})
		return
	}

	// Validate duration
	_, err := models.ParseDuration(req.Duration)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: models.ErrInvalidDuration.Message,
			Code:  models.ErrInvalidDuration.Code,
		})
		return
	}

	// Get client IP
	clientIP := middleware.GetClientIP(c)

	// Initialize upload
	resp, err := h.uploadService.InitUpload(c.Request.Context(), &req, clientIP)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to initialize upload",
			Code:  "UPLOAD_INIT_FAILED",
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Chunk handles POST /api/upload/chunk
func (h *UploadHandler) Chunk(c *gin.Context) {
	// Parse multipart form
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil { // 10MB max in memory
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "Failed to parse multipart form",
			Code:    "PARSE_ERROR",
			Details: err.Error(),
		})
		return
	}

	// Get session ID
	sessionID := c.PostForm("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Missing session_id",
			Code:  "MISSING_SESSION_ID",
		})
		return
	}

	// Get chunk index
	chunkIndexStr := c.PostForm("chunk_index")
	if chunkIndexStr == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Missing chunk_index",
			Code:  "MISSING_CHUNK_INDEX",
		})
		return
	}

	var chunkIndex int
	if _, err := fmt.Sscanf(chunkIndexStr, "%d", &chunkIndex); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "Invalid chunk_index",
			Code:    "INVALID_CHUNK_INDEX",
			Details: err.Error(),
		})
		return
	}

	// Get chunk file
	file, _, err := c.Request.FormFile("chunk")
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "Missing chunk file",
			Code:    "MISSING_CHUNK",
			Details: err.Error(),
		})
		return
	}
	defer file.Close()

	// Upload chunk
	err = h.uploadService.UploadChunk(c.Request.Context(), sessionID, chunkIndex, file)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			status := http.StatusBadRequest
			if appErr == models.ErrSessionNotFound || appErr == models.ErrSessionExpired {
				status = http.StatusNotFound
			}
			c.JSON(status, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to upload chunk",
			Code:  "CHUNK_UPLOAD_FAILED",
		})
		return
	}

	// Get progress
	uploaded, total, err := h.uploadService.GetUploadProgress(c.Request.Context(), sessionID)
	if err != nil {
		// Still return success, just without progress
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"chunk_index": chunkIndex,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"chunk_index":     chunkIndex,
		"uploaded_chunks": uploaded,
		"total_chunks":    total,
	})
}

// Complete handles POST /api/upload/complete
func (h *UploadHandler) Complete(c *gin.Context) {
	var req models.UploadCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "Invalid request body",
			Code:    "INVALID_REQUEST",
			Details: err.Error(),
		})
		return
	}

	if !req.Confirmed {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Upload must be confirmed",
			Code:  "NOT_CONFIRMED",
		})
		return
	}

	// Complete upload
	resp, err := h.uploadService.CompleteUpload(c.Request.Context(), req.SessionID)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			status := http.StatusBadRequest
			if appErr == models.ErrSessionNotFound || appErr == models.ErrSessionExpired {
				status = http.StatusNotFound
			}
			c.JSON(status, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "Failed to complete upload",
			Code:    "COMPLETE_FAILED",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Cancel handles DELETE /api/upload/cancel
func (h *UploadHandler) Cancel(c *gin.Context) {
	var req models.UploadCancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "Invalid request body",
			Code:    "INVALID_REQUEST",
			Details: err.Error(),
		})
		return
	}

	// Cancel upload
	err := h.uploadService.CancelUpload(c.Request.Context(), req.SessionID)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to cancel upload",
			Code:  "CANCEL_FAILED",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Upload cancelled",
	})
}

// Progress handles GET /api/upload/progress/:session_id
func (h *UploadHandler) Progress(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Missing session_id",
			Code:  "MISSING_SESSION_ID",
		})
		return
	}

	uploaded, total, err := h.uploadService.GetUploadProgress(c.Request.Context(), sessionID)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			status := http.StatusBadRequest
			if appErr == models.ErrSessionNotFound {
				status = http.StatusNotFound
			}
			c.JSON(status, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to get progress",
			Code:  "PROGRESS_FAILED",
		})
		return
	}

	percentage := 0.0
	if total > 0 {
		percentage = float64(uploaded) / float64(total) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"uploaded_chunks": uploaded,
		"total_chunks":    total,
		"percentage":      percentage,
	})
}