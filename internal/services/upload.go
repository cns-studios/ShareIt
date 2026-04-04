package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"secureshare/internal/config"
	"secureshare/internal/models"
	"secureshare/internal/storage"
)

type Upload struct {
	cfg      *config.Config
	db       *storage.Postgres
	redis    *storage.Redis
	fs       *storage.Filesystem
	stopChan chan struct{}
	wg       sync.WaitGroup
}

func NewUpload(cfg *config.Config, db *storage.Postgres, redis *storage.Redis, fs *storage.Filesystem) *Upload {
	return &Upload{
		cfg:      cfg,
		db:       db,
		redis:    redis,
		fs:       fs,
		stopChan: make(chan struct{}),
	}
}

// StartPendingCleanup starts the background job to clean up abandoned uploads
func (u *Upload) StartPendingCleanup() {
	u.wg.Add(1)
	go u.runPendingCleanup()
}

// Stop gracefully stops the upload service
func (u *Upload) Stop() {
	close(u.stopChan)
	u.wg.Wait()
}

func (u *Upload) runPendingCleanup() {
	defer u.wg.Done()

	// Run every minute
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			u.cleanupPendingUploads()
		case <-u.stopChan:
			log.Println("Upload service stopping...")
			return
		}
	}
}

func (u *Upload) cleanupPendingUploads() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all sessions from filesystem
	sessionIDs, err := u.fs.GetAllSessionIDs()
	if err != nil {
		log.Printf("Error getting session IDs: %v", err)
		return
	}

	for _, sessionID := range sessionIDs {
		// Check if session still exists in Redis
		session, err := u.redis.GetUploadSession(ctx, sessionID)
		if err == models.ErrSessionNotFound {
			// Session expired, clean up chunks
			log.Printf("Cleaning up expired session: %s", sessionID)
			if err := u.fs.DeleteChunks(sessionID); err != nil {
				log.Printf("Error deleting chunks for session %s: %v", sessionID, err)
			}
			continue
		}
		if err != nil {
			log.Printf("Error getting session %s: %v", sessionID, err)
			continue
		}

		// Check if the associated file is still pending
		isPending, err := u.redis.IsFilePending(ctx, session.FileID)
		if err != nil {
			log.Printf("Error checking pending status for file %s: %v", session.FileID, err)
			continue
		}

		if !isPending {
			// File was confirmed or pending expired, check if file exists in DB
			_, err := u.db.GetFileByID(ctx, session.FileID)
			if err == models.ErrFileNotFound {
				// File was never confirmed, clean up
				log.Printf("Cleaning up abandoned upload: session=%s, file=%s", sessionID, session.FileID)
				u.CleanupSession(ctx, sessionID)
			}
		}
	}
}

// InitUpload initializes a new upload session
func (u *Upload) InitUpload(ctx context.Context, req *models.UploadInitRequest, uploaderIP string) (*models.UploadInitResponse, error) {
	// Validate file size
	if req.FileSize > u.cfg.MaxFileSize {
		return nil, models.ErrFileTooLarge
	}

	// Parse and validate duration
	duration, err := models.ParseDuration(req.Duration)
	if err != nil {
		return nil, err
	}

	// Generate IDs
	sessionID := models.GenerateSessionID()
	fileID := models.GenerateID(17)

	// Generate unique numeric code
	var numericCode string
	for i := 0; i < 10; i++ {
		numericCode = models.GenerateNumericCode()
		exists, err := u.db.NumericCodeExists(ctx, numericCode)
		if err != nil {
			return nil, fmt.Errorf("error checking numeric code: %w", err)
		}
		if !exists {
			break
		}
		if i == 9 {
			return nil, fmt.Errorf("failed to generate unique numeric code")
		}
	}

	// Create upload session
	session := &models.UploadSession{
		SessionID:    sessionID,
		FileID:       fileID,
		OriginalName: req.FileName,
		TotalSize:    req.FileSize,
		TotalChunks:  req.TotalChunks,
		ChunkSize:    req.ChunkSize,
		UploaderIP:   uploaderIP,
		ExpiresAt:    time.Now().Add(duration),
		CreatedAt:    time.Now(),
	}

	// Store session in Redis
	if err := u.redis.CreateUploadSession(ctx, session); err != nil {
		return nil, fmt.Errorf("error creating upload session: %w", err)
	}

	// Create chunk directory
	if err := u.fs.CreateChunkDir(sessionID); err != nil {
		u.redis.DeleteUploadSession(ctx, sessionID)
		return nil, fmt.Errorf("error creating chunk directory: %w", err)
	}

	// Mark file as pending
	if err := u.redis.MarkFilePending(ctx, fileID, sessionID); err != nil {
		u.fs.DeleteChunks(sessionID)
		u.redis.DeleteUploadSession(ctx, sessionID)
		return nil, fmt.Errorf("error marking file as pending: %w", err)
	}

	return &models.UploadInitResponse{
		SessionID:   sessionID,
		FileID:      fileID,
		ChunkSize:   req.ChunkSize,
		TotalChunks: req.TotalChunks,
	}, nil
}

// UploadChunk handles uploading a single chunk
func (u *Upload) UploadChunk(ctx context.Context, sessionID string, chunkIndex int, data io.Reader) error {
	// Get session
	session, err := u.redis.GetUploadSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Validate chunk index
	if chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		return models.ErrInvalidChunk
	}

	// Check if chunk already uploaded
	exists, err := u.redis.IsChunkUploaded(ctx, sessionID, chunkIndex)
	if err != nil {
		return err
	}
	if exists {
		return models.ErrChunkAlreadyExists
	}

	// Save chunk to filesystem
	_, err = u.fs.SaveChunk(sessionID, chunkIndex, data)
	if err != nil {
		return fmt.Errorf("error saving chunk: %w", err)
	}

	// Mark chunk as uploaded in Redis
	if err := u.redis.MarkChunkUploaded(ctx, sessionID, chunkIndex); err != nil {
		// Try to clean up the saved chunk
		// fs.DeleteChunk is not implemented, but chunks will be cleaned up eventually
		return fmt.Errorf("error marking chunk as uploaded: %w", err)
	}

	// Extend session TTL
	if err := u.redis.ExtendUploadSession(ctx, sessionID); err != nil {
		log.Printf("Warning: failed to extend session TTL: %v", err)
	}

	return nil
}

// CompleteUpload finalizes the upload and assembles chunks
func (u *Upload) CompleteUpload(ctx context.Context, sessionID string) (*models.UploadCompleteResponse, error) {
	// Get session
	session, err := u.redis.GetUploadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Check all chunks are uploaded
	uploadedCount, err := u.redis.GetUploadedChunkCount(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if int(uploadedCount) != session.TotalChunks {
		return nil, models.ErrUploadIncomplete
	}

	// Assemble chunks into final file
	if err := u.fs.AssembleChunks(sessionID, session.FileID, session.TotalChunks); err != nil {
		return nil, fmt.Errorf("error assembling chunks: %w", err)
	}

	// Get actual file size
	actualSize, err := u.fs.GetFileSize(session.FileID)
	if err != nil {
		return nil, fmt.Errorf("error getting file size: %w", err)
	}

	// Generate numeric code for this file
	var numericCode string
	for i := 0; i < 10; i++ {
		numericCode = models.GenerateNumericCode()
		exists, err := u.db.NumericCodeExists(ctx, numericCode)
		if err != nil {
			return nil, fmt.Errorf("error checking numeric code: %w", err)
		}
		if !exists {
			break
		}
	}

	// Create file record in database
	file := &models.File{
		ID:           session.FileID,
		NumericCode:  numericCode,
		OriginalName: session.OriginalName,
		SizeBytes:    actualSize,
		UploaderIP:   session.UploaderIP,
		ExpiresAt:    session.ExpiresAt,
		CreatedAt:    time.Now(),
	}

	if err := u.db.CreateFile(ctx, file); err != nil {
		// Clean up on error
		u.fs.DeleteFile(session.FileID)
		return nil, fmt.Errorf("error creating file record: %w", err)
	}

	// Clean up Redis data
	u.redis.RemovePendingFile(ctx, session.FileID)
	u.redis.DeleteUploadSession(ctx, sessionID)
	u.redis.DeleteChunkTracking(ctx, sessionID)

	// Build share URL
	shareURL := fmt.Sprintf("%s/shared/%s", u.cfg.BaseURL, session.FileID)

	return &models.UploadCompleteResponse{
		FileID:      session.FileID,
		NumericCode: numericCode,
		ShareURL:    shareURL,
	}, nil
}

// CancelUpload cancels an in-progress upload
func (u *Upload) CancelUpload(ctx context.Context, sessionID string) error {
	// Get session to find file ID
	session, err := u.redis.GetUploadSession(ctx, sessionID)
	if err != nil && err != models.ErrSessionNotFound {
		return err
	}

	// Clean up chunks
	if err := u.fs.DeleteChunks(sessionID); err != nil {
		log.Printf("Error deleting chunks for session %s: %v", sessionID, err)
	}

	// Clean up Redis
	if session != nil {
		u.redis.RemovePendingFile(ctx, session.FileID)
	}
	u.redis.DeleteUploadSession(ctx, sessionID)
	u.redis.DeleteChunkTracking(ctx, sessionID)

	return nil
}

// CleanupSession removes all traces of an upload session
func (u *Upload) CleanupSession(ctx context.Context, sessionID string) {
	session, _ := u.redis.GetUploadSession(ctx, sessionID)
	
	// Delete chunks
	if err := u.fs.DeleteChunks(sessionID); err != nil {
		log.Printf("Error deleting chunks for session %s: %v", sessionID, err)
	}

	// Delete file if it exists and wasn't committed
	if session != nil {
		if u.fs.FileExists(session.FileID) {
			_, err := u.db.GetFileByID(ctx, session.FileID)
			if err == models.ErrFileNotFound {
				// File wasn't committed, safe to delete
				u.fs.DeleteFile(session.FileID)
			}
		}
		u.redis.RemovePendingFile(ctx, session.FileID)
	}

	// Clean up Redis
	u.redis.DeleteUploadSession(ctx, sessionID)
	u.redis.DeleteChunkTracking(ctx, sessionID)
}

// GetUploadProgress returns the current upload progress
func (u *Upload) GetUploadProgress(ctx context.Context, sessionID string) (int, int, error) {
	session, err := u.redis.GetUploadSession(ctx, sessionID)
	if err != nil {
		return 0, 0, err
	}

	uploadedCount, err := u.redis.GetUploadedChunkCount(ctx, sessionID)
	if err != nil {
		return 0, 0, err
	}

	return int(uploadedCount), session.TotalChunks, nil
}