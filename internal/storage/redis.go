package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"secureshare/internal/config"
	"secureshare/internal/models"

	"github.com/go-redis/redis/v8"
)

type Redis struct {
	client *redis.Client
}

// Key prefixes
const (
	prefixUploadSession = "upload:session:"
	prefixUploadChunks  = "upload:chunks:"
	prefixPendingFile   = "pending:file:"
	prefixRateLimit     = "ratelimit:"
)

// TTLs
const (
	sessionTTL     = 1 * time.Hour
	pendingTTL     = 10 * time.Minute
	rateLimitTTL   = 1 * time.Minute
	rateLimitMax   = 10 // Max uploads per minute per IP
)

func NewRedis(cfg *config.Config) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:        cfg.RedisAddr(),
		DB:          0,
		DialTimeout: 5 * time.Second,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &Redis{client: client}, nil
}

func (r *Redis) Close() error {
	return r.client.Close()
}

func (r *Redis) Client() *redis.Client {
	return r.client
}

// ---- Upload Session Management ----

// CreateUploadSession stores a new upload session
func (r *Redis) CreateUploadSession(ctx context.Context, session *models.UploadSession) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	key := prefixUploadSession + session.SessionID
	return r.client.Set(ctx, key, data, sessionTTL).Err()
}

// GetUploadSession retrieves an upload session
func (r *Redis) GetUploadSession(ctx context.Context, sessionID string) (*models.UploadSession, error) {
	key := prefixUploadSession + sessionID
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, models.ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	var session models.UploadSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// DeleteUploadSession removes an upload session
func (r *Redis) DeleteUploadSession(ctx context.Context, sessionID string) error {
	key := prefixUploadSession + sessionID
	return r.client.Del(ctx, key).Err()
}

// ExtendUploadSession extends the TTL of an upload session
func (r *Redis) ExtendUploadSession(ctx context.Context, sessionID string) error {
	key := prefixUploadSession + sessionID
	return r.client.Expire(ctx, key, sessionTTL).Err()
}

// ---- Chunk Tracking ----

// MarkChunkUploaded marks a specific chunk as uploaded
func (r *Redis) MarkChunkUploaded(ctx context.Context, sessionID string, chunkIndex int) error {
	key := prefixUploadChunks + sessionID
	pipe := r.client.Pipeline()
	pipe.SAdd(ctx, key, chunkIndex)
	pipe.Expire(ctx, key, sessionTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// IsChunkUploaded checks if a chunk has been uploaded
func (r *Redis) IsChunkUploaded(ctx context.Context, sessionID string, chunkIndex int) (bool, error) {
	key := prefixUploadChunks + sessionID
	return r.client.SIsMember(ctx, key, chunkIndex).Result()
}

// GetUploadedChunks returns all uploaded chunk indices
func (r *Redis) GetUploadedChunks(ctx context.Context, sessionID string) ([]int, error) {
	key := prefixUploadChunks + sessionID
	members, err := r.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	chunks := make([]int, 0, len(members))
	for _, m := range members {
		var idx int
		fmt.Sscanf(m, "%d", &idx)
		chunks = append(chunks, idx)
	}
	return chunks, nil
}

// GetUploadedChunkCount returns the number of uploaded chunks
func (r *Redis) GetUploadedChunkCount(ctx context.Context, sessionID string) (int64, error) {
	key := prefixUploadChunks + sessionID
	return r.client.SCard(ctx, key).Result()
}

// DeleteChunkTracking removes chunk tracking for a session
func (r *Redis) DeleteChunkTracking(ctx context.Context, sessionID string) error {
	key := prefixUploadChunks + sessionID
	return r.client.Del(ctx, key).Err()
}

// SetChunkTrackingTTL sets TTL for chunk tracking
func (r *Redis) SetChunkTrackingTTL(ctx context.Context, sessionID string) error {
	key := prefixUploadChunks + sessionID
	return r.client.Expire(ctx, key, sessionTTL).Err()
}

// ---- Pending File Management ----

// MarkFilePending marks a file as pending confirmation
func (r *Redis) MarkFilePending(ctx context.Context, fileID, sessionID string) error {
	key := prefixPendingFile + fileID
	return r.client.Set(ctx, key, sessionID, pendingTTL).Err()
}

// IsFilePending checks if a file is pending confirmation
func (r *Redis) IsFilePending(ctx context.Context, fileID string) (bool, error) {
	key := prefixPendingFile + fileID
	exists, err := r.client.Exists(ctx, key).Result()
	return exists > 0, err
}

// RemovePendingFile removes a file from pending status
func (r *Redis) RemovePendingFile(ctx context.Context, fileID string) error {
	key := prefixPendingFile + fileID
	return r.client.Del(ctx, key).Err()
}

// GetAllPendingFiles returns all pending file IDs
func (r *Redis) GetAllPendingFiles(ctx context.Context) ([]string, error) {
	pattern := prefixPendingFile + "*"
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	fileIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		fileID := key[len(prefixPendingFile):]
		fileIDs = append(fileIDs, fileID)
	}
	return fileIDs, nil
}

// ---- Rate Limiting ----

// CheckRateLimit checks if an IP is rate limited, returns true if allowed
func (r *Redis) CheckRateLimit(ctx context.Context, ip string) (bool, error) {
	key := prefixRateLimit + ip
	
	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, rateLimitTTL)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	return incr.Val() <= rateLimitMax, nil
}

// GetRateLimitCount returns current request count for an IP
func (r *Redis) GetRateLimitCount(ctx context.Context, ip string) (int64, error) {
	key := prefixRateLimit + ip
	count, err := r.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return count, err
}

// ---- Cleanup ----

// CleanupSession removes all data related to an upload session
func (r *Redis) CleanupSession(ctx context.Context, sessionID string) error {
	// Get session to find file ID
	session, err := r.GetUploadSession(ctx, sessionID)
	if err != nil && err != models.ErrSessionNotFound {
		return err
	}

	// Delete session
	if err := r.DeleteUploadSession(ctx, sessionID); err != nil {
		return err
	}

	// Delete chunk tracking
	if err := r.DeleteChunkTracking(ctx, sessionID); err != nil {
		return err
	}

	// Remove pending file if exists
	if session != nil {
		if err := r.RemovePendingFile(ctx, session.FileID); err != nil {
			return err
		}
	}

	return nil
}

// GetAllActiveSessions returns all active session IDs
func (r *Redis) GetAllActiveSessions(ctx context.Context) ([]string, error) {
	pattern := prefixUploadSession + "*"
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	sessionIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		sessionID := key[len(prefixUploadSession):]
		sessionIDs = append(sessionIDs, sessionID)
	}
	return sessionIDs, nil
}