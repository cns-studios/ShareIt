-- Database initialization script for PostgreSQL

-- Create files table
CREATE TABLE IF NOT EXISTS files (
    id VARCHAR(20) PRIMARY KEY,
    numeric_code VARCHAR(12) UNIQUE NOT NULL,
    original_name TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    uploader_ip VARCHAR(45),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    report_count INT DEFAULT 0,
    is_deleted BOOLEAN DEFAULT FALSE
);

-- Create reports table
CREATE TABLE IF NOT EXISTS reports (
    id SERIAL PRIMARY KEY,
    file_id VARCHAR(20) REFERENCES files(id) ON DELETE CASCADE,
    reporter_ip VARCHAR(45),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_files_numeric_code ON files(numeric_code);
CREATE INDEX IF NOT EXISTS idx_files_expires_at ON files(expires_at);
CREATE INDEX IF NOT EXISTS idx_files_is_deleted ON files(is_deleted);
CREATE INDEX IF NOT EXISTS idx_files_created_at ON files(created_at);
CREATE INDEX IF NOT EXISTS idx_reports_file_id ON reports(file_id);

-- Function to auto-delete files when report count exceeds threshold
-- This will be called from the application, not as a trigger
-- (threshold is configurable via environment variable)

-- Create a function to clean up expired files (called by app)
CREATE OR REPLACE FUNCTION cleanup_expired_files()
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    WITH deleted AS (
        UPDATE files 
        SET is_deleted = TRUE 
        WHERE expires_at < NOW() 
        AND is_deleted = FALSE
        RETURNING id
    )
    SELECT COUNT(*) INTO deleted_count FROM deleted;
    
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;