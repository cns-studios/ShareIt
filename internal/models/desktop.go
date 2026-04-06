package models

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type DesktopAPIKey struct {
	ID        string    `db:"id" json:"id"`
	KeyValue  string    `db:"key_value" json:"key_value"`
	OwnerName string    `db:"owner_name" json:"owner_name"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	IsActive  bool      `db:"is_active" json:"is_active"`
}

type DesktopFile struct {
	FileID   string `db:"file_id"`
	APIKeyID string `db:"api_key_id"`
}

type DesktopFileMetadata struct {
	ID           string    `json:"id"`
	NumericCode  string    `json:"numeric_code"`
	FileName     string    `json:"file_name"`
	FileSize     int64     `json:"file_size"`
	ExpiresAt    time.Time `json:"expires_at"`
	UploadedAt   time.Time `json:"uploaded_at"`
}


type DesktopVerifyResponse struct {
	Status    string `json:"status"`
	Owner     string `json:"owner"`
}

type DesktopFinalizeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Duration  string `json:"duration" binding:"required"`
}
type DesktopFinalizeResponse struct {
	FileID      string    `json:"file_id"`
	NumericCode string    `json:"numeric_code"`
	FileName    string    `json:"file_name"`
	FileSize    int64     `json:"file_size"`
	ExpiresAt   time.Time `json:"expires_at"`
	ShareURL    string    `json:"share_url"`
}

func GenerateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var (
	ErrAPIKeyNotFound = &AppError{Code: "API_KEY_NOT_FOUND", Message: "API key not found or inactive"}
	ErrFileNotOwned   = &AppError{Code: "FILE_NOT_OWNED", Message: "file does not belong to this API key"}
)