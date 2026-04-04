package handlers

import (
	"net/http"
	"time"

	"secureshare/internal/config"
	"secureshare/internal/middleware"
	"secureshare/internal/models"
	"secureshare/internal/services"
	"secureshare/internal/storage"

	"github.com/gin-gonic/gin"
)

type ReportHandler struct {
	cfg     *config.Config
	db      *storage.Postgres
	discord *services.Discord
}

func NewReportHandler(cfg *config.Config, db *storage.Postgres, discord *services.Discord) *ReportHandler {
	return &ReportHandler{
		cfg:     cfg,
		db:      db,
		discord: discord,
	}
}

// Report handles POST /api/file/:id/report
func (h *ReportHandler) Report(c *gin.Context) {
	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error: "Missing file ID",
			Code:  "MISSING_FILE_ID",
		})
		return
	}

	// Get client IP
	reporterIP := middleware.GetClientIP(c)

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

	// Check if user has already reported this file
	hasReported, err := h.db.HasUserReportedFile(c.Request.Context(), fileID, reporterIP)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to check report status",
			Code:  "CHECK_REPORT_FAILED",
		})
		return
	}

	if hasReported {
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error: "You have already reported this file",
			Code:  "ALREADY_REPORTED",
		})
		return
	}

	// Create report record
	report := &models.Report{
		FileID:     fileID,
		ReporterIP: reporterIP,
		CreatedAt:  time.Now(),
	}

	if err := h.db.CreateReport(c.Request.Context(), report); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to create report",
			Code:  "CREATE_REPORT_FAILED",
		})
		return
	}

	// Increment report count
	newReportCount, err := h.db.IncrementReportCount(c.Request.Context(), fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error: "Failed to update report count",
			Code:  "UPDATE_REPORT_FAILED",
		})
		return
	}

	// Update file object with new report count for notification
	file.ReportCount = newReportCount

	// Send Discord notification
	if err := h.discord.SendReportNotification(file, reporterIP, newReportCount); err != nil {
		// Log error but don't fail the request
		println("Failed to send Discord notification:", err.Error())
	}

	// Check if file should be auto-deleted
	if newReportCount >= h.cfg.AutoDeleteReportCount {
		// Mark file as deleted
		if err := h.db.MarkFileDeleted(c.Request.Context(), fileID); err != nil {
			// Log error but don't fail the request
			println("Failed to mark file as deleted:", err.Error())
		} else {
			// Send auto-delete notification
			if err := h.discord.SendAutoDeleteNotification(file); err != nil {
				println("Failed to send auto-delete notification:", err.Error())
			}
		}

		c.JSON(http.StatusOK, models.ReportResponse{
			Success: true,
			Message: "File has been reported and automatically removed due to multiple reports",
		})
		return
	}

	c.JSON(http.StatusOK, models.ReportResponse{
		Success: true,
		Message: "File has been reported. Thank you for helping keep our platform safe.",
	})
}