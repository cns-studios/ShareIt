package handlers

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"secureshare/internal/config"
	"secureshare/internal/models"
	"secureshare/internal/storage"

	"github.com/gin-gonic/gin"
)

type DownloadHandler struct {
	cfg *config.Config
	db  *storage.Postgres
	fs  *storage.Filesystem
}

func NewDownloadHandler(cfg *config.Config, db *storage.Postgres, fs *storage.Filesystem) *DownloadHandler {
	return &DownloadHandler{
		cfg: cfg,
		db:  db,
		fs:  fs,
	}
}

// GetMetadata handles GET /api/file/:id
func (h *DownloadHandler) GetMetadata(c *gin.Context) {
	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Missing file ID",
			Code:  "MISSING_FILE_ID",
		})
		return
	}

	// Validate file ID format
	if !isValidFileID(fileID) {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Invalid file ID format",
			Code:  "INVALID_FILE_ID",
		})
		return
	}

	// Get file from database
	file, err := h.db.GetFileByID(c.Request.Context(), fileID)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			status := http.StatusNotFound
			if appErr == models.ErrFileExpired || appErr == models.ErrFileDeleted {
				status = http.StatusGone
			}
			c.JSON(status, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to get file",
			Code:  "GET_FILE_FAILED",
		})
		return
	}

	// Check if file exists on disk
	if !h.fs.FileExists(fileID) {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error: "File not found on storage",
			Code:  "FILE_NOT_ON_DISK",
		})
		return
	}

	c.JSON(http.StatusOK, file.ToMetadata())
}

// Download handles GET /api/file/:id/download
func (h *DownloadHandler) Download(c *gin.Context) {
	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Missing file ID",
			Code:  "MISSING_FILE_ID",
		})
		return
	}

	// Validate file ID format
	if !isValidFileID(fileID) {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Invalid file ID format",
			Code:  "INVALID_FILE_ID",
		})
		return
	}

	// Get file from database
	file, err := h.db.GetFileByID(c.Request.Context(), fileID)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			status := http.StatusNotFound
			if appErr == models.ErrFileExpired || appErr == models.ErrFileDeleted {
				status = http.StatusGone
			}
			c.JSON(status, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to get file",
			Code:  "GET_FILE_FAILED",
		})
		return
	}

	// Open file for reading
	reader, err := h.fs.GetFileReader(fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error: "File not found on storage",
			Code:  "FILE_NOT_ON_DISK",
		})
		return
	}
	defer reader.Close()

	// Get file size
	fileSize, err := h.fs.GetFileSize(fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to get file size",
			Code:  "FILE_SIZE_ERROR",
		})
		return
	}

	// Set headers for download
	// Note: We use .enc extension since the file is encrypted
	// The original name is sent in a custom header for the frontend to use
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.enc\"", fileID))
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Length", fmt.Sprintf("%d", fileSize))
	c.Header("X-Original-Filename", file.OriginalName)
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")

	// Stream file to response
	c.Status(http.StatusOK)
	_, err = io.Copy(c.Writer, reader)
	if err != nil {
		// Can't send error response at this point, just log
		fmt.Printf("Error streaming file %s: %v\n", fileID, err)
	}
}

// GetByCode handles GET /api/file/code/:code
func (h *DownloadHandler) GetByCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Missing numeric code",
			Code:  "MISSING_CODE",
		})
		return
	}

	// Validate code format (12 digits)
	if !isValidNumericCode(code) {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Invalid numeric code format. Must be 12 digits.",
			Code:  "INVALID_CODE_FORMAT",
		})
		return
	}

	// Get file from database
	file, err := h.db.GetFileByNumericCode(c.Request.Context(), code)
	if err != nil {
		if appErr, ok := err.(*models.AppError); ok {
			status := http.StatusNotFound
			if appErr == models.ErrFileExpired || appErr == models.ErrFileDeleted {
				status = http.StatusGone
			}
			c.JSON(status, models.ErrorResponse{
				Error: appErr.Message,
				Code:  appErr.Code,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to get file",
			Code:  "GET_FILE_FAILED",
		})
		return
	}

	// Check if file exists on disk
	if !h.fs.FileExists(file.ID) {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error: "File not found on storage",
			Code:  "FILE_NOT_ON_DISK",
		})
		return
	}

	c.JSON(http.StatusOK, file.ToMetadata())
}

// isValidFileID checks if the file ID has the expected format
func isValidFileID(id string) bool {
	// File IDs are 17 character alphanumeric strings
	if len(id) != 17 {
		return false
	}
	matched, _ := regexp.MatchString("^[a-z0-9]+$", id)
	return matched
}

// isValidNumericCode checks if the code is exactly 12 digits
func isValidNumericCode(code string) bool {
	if len(code) != 12 {
		return false
	}
	matched, _ := regexp.MatchString("^[0-9]+$", code)
	return matched
}