package storage

import (
	"context"
	"database/sql"
	"time"

	"secureshare/internal/config"
	"secureshare/internal/models"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

type Postgres struct {
	db *sqlx.DB
}

func NewPostgres(cfg *config.Config) (*Postgres, error) {
	db, err := sqlx.Connect("postgres", cfg.PostgresDSN())
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	return &Postgres{db: db}, nil
}

func (p *Postgres) Close() error {
	return p.db.Close()
}

// CreateFile inserts a new file record
func (p *Postgres) CreateFile(ctx context.Context, file *models.File) error {
	query := `
		INSERT INTO files (id, numeric_code, original_name, size_bytes, uploader_ip, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := p.db.ExecContext(ctx, query,
		file.ID,
		file.NumericCode,
		file.OriginalName,
		file.SizeBytes,
		file.UploaderIP,
		file.ExpiresAt,
		file.CreatedAt,
	)
	return err
}

// GetFileByID retrieves a file by its ID
func (p *Postgres) GetFileByID(ctx context.Context, id string) (*models.File, error) {
	var file models.File
	query := `SELECT * FROM files WHERE id = $1`
	err := p.db.GetContext(ctx, &file, query, id)
	if err == sql.ErrNoRows {
		return nil, models.ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}

	// Check if deleted
	if file.IsDeleted {
		return nil, models.ErrFileDeleted
	}

	// Check if expired
	if time.Now().After(file.ExpiresAt) {
		return nil, models.ErrFileExpired
	}

	return &file, nil
}

// GetFileByNumericCode retrieves a file by its 12-digit numeric code
func (p *Postgres) GetFileByNumericCode(ctx context.Context, code string) (*models.File, error) {
	var file models.File
	query := `SELECT * FROM files WHERE numeric_code = $1`
	err := p.db.GetContext(ctx, &file, query, code)
	if err == sql.ErrNoRows {
		return nil, models.ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}

	// Check if deleted
	if file.IsDeleted {
		return nil, models.ErrFileDeleted
	}

	// Check if expired
	if time.Now().After(file.ExpiresAt) {
		return nil, models.ErrFileExpired
	}

	return &file, nil
}

// IncrementReportCount increments the report count for a file
func (p *Postgres) IncrementReportCount(ctx context.Context, fileID string) (int, error) {
	var reportCount int
	query := `
		UPDATE files 
		SET report_count = report_count + 1 
		WHERE id = $1 
		RETURNING report_count
	`
	err := p.db.GetContext(ctx, &reportCount, query, fileID)
	return reportCount, err
}

// MarkFileDeleted marks a file as deleted
func (p *Postgres) MarkFileDeleted(ctx context.Context, fileID string) error {
	query := `UPDATE files SET is_deleted = TRUE WHERE id = $1`
	_, err := p.db.ExecContext(ctx, query, fileID)
	return err
}

// CreateReport creates a new report record
func (p *Postgres) CreateReport(ctx context.Context, report *models.Report) error {
	query := `
		INSERT INTO reports (file_id, reporter_ip, created_at)
		VALUES ($1, $2, $3)
	`
	_, err := p.db.ExecContext(ctx, query,
		report.FileID,
		report.ReporterIP,
		report.CreatedAt,
	)
	return err
}

// GetReportsByFileID retrieves all reports for a file
func (p *Postgres) GetReportsByFileID(ctx context.Context, fileID string) ([]models.Report, error) {
	var reports []models.Report
	query := `SELECT * FROM reports WHERE file_id = $1 ORDER BY created_at DESC`
	err := p.db.SelectContext(ctx, &reports, query, fileID)
	return reports, err
}

// HasUserReportedFile checks if an IP has already reported a specific file
func (p *Postgres) HasUserReportedFile(ctx context.Context, fileID, reporterIP string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM reports WHERE file_id = $1 AND reporter_ip = $2`
	err := p.db.GetContext(ctx, &count, query, fileID, reporterIP)
	return count > 0, err
}

// GetExpiredFiles retrieves files that have expired and are not deleted
func (p *Postgres) GetExpiredFiles(ctx context.Context) ([]models.File, error) {
	var files []models.File
	query := `SELECT * FROM files WHERE expires_at < $1 AND is_deleted = FALSE`
	err := p.db.SelectContext(ctx, &files, query, time.Now())
	return files, err
}

// DeleteExpiredFiles marks all expired files as deleted and returns count
func (p *Postgres) DeleteExpiredFiles(ctx context.Context) (int64, error) {
	query := `UPDATE files SET is_deleted = TRUE WHERE expires_at < $1 AND is_deleted = FALSE`
	result, err := p.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetDeletedFiles retrieves all files marked as deleted that may still have blobs on disk
func (p *Postgres) GetDeletedFiles(ctx context.Context) ([]models.File, error) {
	var files []models.File
	query := `SELECT * FROM files WHERE is_deleted = TRUE`
	err := p.db.SelectContext(ctx, &files, query)
	return files, err
}

// GetFileForAdmin retrieves a file by ID for admin purposes (includes deleted/expired)
func (p *Postgres) GetFileForAdmin(ctx context.Context, id string) (*models.File, error) {
	var file models.File
	query := `SELECT * FROM files WHERE id = $1`
	err := p.db.GetContext(ctx, &file, query, id)
	if err == sql.ErrNoRows {
		return nil, models.ErrFileNotFound
	}
	return &file, err
}

// GetAllFiles retrieves all files for admin purposes
func (p *Postgres) GetAllFiles(ctx context.Context, limit, offset int) ([]models.File, error) {
	var files []models.File
	query := `SELECT * FROM files ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	err := p.db.SelectContext(ctx, &files, query, limit, offset)
	return files, err
}

// DeleteFilePermanently permanently removes a file record
func (p *Postgres) DeleteFilePermanently(ctx context.Context, fileID string) error {
	query := `DELETE FROM files WHERE id = $1`
	_, err := p.db.ExecContext(ctx, query, fileID)
	return err
}

// NumericCodeExists checks if a numeric code already exists
func (p *Postgres) NumericCodeExists(ctx context.Context, code string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM files WHERE numeric_code = $1`
	err := p.db.GetContext(ctx, &count, query, code)
	return count > 0, err
}

// GetStats retrieves basic statistics
func (p *Postgres) GetStats(ctx context.Context) (totalFiles, activeFiles, totalReports int64, totalSize int64, err error) {
	err = p.db.GetContext(ctx, &totalFiles, `SELECT COUNT(*) FROM files`)
	if err != nil {
		return
	}

	err = p.db.GetContext(ctx, &activeFiles, `SELECT COUNT(*) FROM files WHERE is_deleted = FALSE AND expires_at > $1`, time.Now())
	if err != nil {
		return
	}

	err = p.db.GetContext(ctx, &totalReports, `SELECT COUNT(*) FROM reports`)
	if err != nil {
		return
	}

	err = p.db.GetContext(ctx, &totalSize, `SELECT COALESCE(SUM(size_bytes), 0) FROM files WHERE is_deleted = FALSE`)
	return
}