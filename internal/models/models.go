package models

import (
	"crypto/rand"
	"math/big"
	"time"
)

// File represents a stored encrypted file
type File struct {
	ID          string    `db:"id" json:"id"`
	NumericCode string    `db:"numeric_code" json:"numeric_code"`
	OriginalName string   `db:"original_name" json:"original_name"`
	SizeBytes   int64     `db:"size_bytes" json:"size_bytes"`
	UploaderIP  string    `db:"uploader_ip" json:"-"`
	ExpiresAt   time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	ReportCount int       `db:"report_count" json:"-"`
	IsDeleted   bool      `db:"is_deleted" json:"-"`
}

// FileMetadata is the public metadata returned to clients
type FileMetadata struct {
	ID           string    `json:"id"`
	NumericCode  string    `json:"numeric_code"`
	OriginalName string    `json:"original_name"`
	SizeBytes    int64     `json:"size_bytes"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// ToMetadata converts a File to public FileMetadata
func (f *File) ToMetadata() *FileMetadata {
	return &FileMetadata{
		ID:           f.ID,
		NumericCode:  f.NumericCode,
		OriginalName: f.OriginalName,
		SizeBytes:    f.SizeBytes,
		ExpiresAt:    f.ExpiresAt,
		CreatedAt:    f.CreatedAt,
	}
}

// Report represents a file report
type Report struct {
	ID         int       `db:"id" json:"id"`
	FileID     string    `db:"file_id" json:"file_id"`
	ReporterIP string    `db:"reporter_ip" json:"reporter_ip"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

// UploadSession represents an active upload session stored in Redis
type UploadSession struct {
	SessionID    string    `json:"session_id"`
	FileID       string    `json:"file_id"`
	OriginalName string    `json:"original_name"`
	TotalSize    int64     `json:"total_size"`
	TotalChunks  int       `json:"total_chunks"`
	ChunkSize    int64     `json:"chunk_size"`
	UploaderIP   string    `json:"uploader_ip"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// UploadInitRequest is the request body for initializing an upload
type UploadInitRequest struct {
	FileName   string `json:"file_name" binding:"required"`
	FileSize   int64  `json:"file_size" binding:"required,gt=0"`
	TotalChunks int   `json:"total_chunks" binding:"required,gt=0"`
	ChunkSize  int64  `json:"chunk_size" binding:"required,gt=0"`
	Duration   string `json:"duration" binding:"required"` // 24h, 7d, 30d, 90d
}

// UploadInitResponse is the response for a successful upload initialization
type UploadInitResponse struct {
	SessionID   string `json:"session_id"`
	FileID      string `json:"file_id"`
	ChunkSize   int64  `json:"chunk_size"`
	TotalChunks int    `json:"total_chunks"`
}

// UploadChunkRequest represents chunk upload metadata (actual data comes as multipart)
type UploadChunkRequest struct {
	SessionID  string `form:"session_id" binding:"required"`
	ChunkIndex int    `form:"chunk_index" binding:"gte=0"`
}

// UploadCompleteRequest is the request to finalize an upload
type UploadCompleteRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Confirmed bool   `json:"confirmed" binding:"required"`
}

// UploadCompleteResponse is the response after completing an upload
type UploadCompleteResponse struct {
	FileID      string `json:"file_id"`
	NumericCode string `json:"numeric_code"`
	ShareURL    string `json:"share_url"`
}

// UploadCancelRequest is the request to cancel an upload
type UploadCancelRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// ReportRequest is the request body for reporting a file
type ReportRequest struct {
	FileID string `json:"file_id" binding:"required"`
}

// ReportResponse is the response after reporting a file
type ReportResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DiscordWebhookPayload represents the Discord webhook message format
type DiscordWebhookPayload struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// Embed represents a Discord embed
type Embed struct {
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Color       int     `json:"color,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
	Timestamp   string  `json:"timestamp,omitempty"`
}

// Field represents a Discord embed field
type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// ErrorResponse is a standard error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// Duration constants
const (
	Duration24Hours  = "24h"
	Duration7Days    = "7d"
	Duration30Days   = "30d"
	Duration90Days   = "90d"
)

// ParseDuration converts duration string to time.Duration
func ParseDuration(d string) (time.Duration, error) {
	switch d {
	case Duration24Hours:
		return 24 * time.Hour, nil
	case Duration7Days:
		return 7 * 24 * time.Hour, nil
	case Duration30Days:
		return 30 * 24 * time.Hour, nil
	case Duration90Days:
		return 90 * 24 * time.Hour, nil
	default:
		return 0, ErrInvalidDuration
	}
}

// Custom errors
type AppError struct {
	Code    string
	Message string
}

func (e *AppError) Error() string {
	return e.Message
}

var (
	ErrInvalidDuration   = &AppError{Code: "INVALID_DURATION", Message: "invalid duration specified"}
	ErrFileTooLarge      = &AppError{Code: "FILE_TOO_LARGE", Message: "file exceeds maximum size limit"}
	ErrFileNotFound      = &AppError{Code: "FILE_NOT_FOUND", Message: "file not found"}
	ErrFileExpired       = &AppError{Code: "FILE_EXPIRED", Message: "file has expired"}
	ErrFileDeleted       = &AppError{Code: "FILE_DELETED", Message: "file has been deleted"}
	ErrSessionNotFound   = &AppError{Code: "SESSION_NOT_FOUND", Message: "upload session not found"}
	ErrSessionExpired    = &AppError{Code: "SESSION_EXPIRED", Message: "upload session has expired"}
	ErrInvalidChunk      = &AppError{Code: "INVALID_CHUNK", Message: "invalid chunk index"}
	ErrChunkAlreadyExists = &AppError{Code: "CHUNK_EXISTS", Message: "chunk already uploaded"}
	ErrUploadIncomplete  = &AppError{Code: "UPLOAD_INCOMPLETE", Message: "not all chunks have been uploaded"}
	ErrRateLimited       = &AppError{Code: "RATE_LIMITED", Message: "too many requests, please slow down"}
	ErrInvalidCode       = &AppError{Code: "INVALID_CODE", Message: "invalid numeric code"}
)

// GenerateID generates a random alphanumeric ID
func GenerateID(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[num.Int64()]
	}
	return string(result)
}

// GenerateNumericCode generates a 12-digit numeric code
func GenerateNumericCode() string {
	const charset = "0123456789"
	result := make([]byte, 12)
	for i := range result {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[num.Int64()]
	}
	return string(result)
}

// GenerateSessionID generates a unique session ID
func GenerateSessionID() string {
	return GenerateID(32)
}