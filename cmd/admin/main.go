package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"shareit/internal/config"
	"shareit/internal/services"
	"shareit/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	db, err := storage.NewPostgres(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to PostgreSQL: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fs, err := storage.NewFilesystem(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing filesystem: %v\n", err)
		os.Exit(1)
	}

	discord := services.NewDiscord(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	command := os.Args[1]

	switch command {
	case "view":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: admin view <file_id>")
			os.Exit(1)
		}
		viewFile(ctx, db, fs, discord, os.Args[2])

	case "delete":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: admin delete <file_id>")
			os.Exit(1)
		}
		deleteFile(ctx, db, fs, discord, os.Args[2])

	case "download":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: admin download <file_id> [output_path]")
			os.Exit(1)
		}
		outputPath := ""
		if len(os.Args) >= 4 {
			outputPath = os.Args[3]
		}
		downloadFile(ctx, db, fs, discord, os.Args[2], outputPath)

	case "list":
		limit := 20
		offset := 0
		if len(os.Args) >= 3 {
			fmt.Sscanf(os.Args[2], "%d", &limit)
		}
		if len(os.Args) >= 4 {
			fmt.Sscanf(os.Args[3], "%d", &offset)
		}
		listFiles(ctx, db, limit, offset)

	case "stats":
		showStats(ctx, db, fs)

	case "reports":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: admin reports <file_id>")
			os.Exit(1)
		}
		showReports(ctx, db, os.Args[2])

	case "cleanup":
		forceCleanup(ctx, cfg, db, fs)

	case "help":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	usage := `
ShareIt Admin CLI

Usage: admin <command> [arguments]

Commands:
  view <file_id>              View file metadata
  delete <file_id>            Delete a file
  download <file_id> [path]   Download encrypted file (optionally specify output path)
  list [limit] [offset]       List files (default: limit=20, offset=0)
  stats                       Show system statistics
  reports <file_id>           Show reports for a file
  cleanup                     Force cleanup of expired files
  help                        Show this help message

Examples:
  admin view abc123def456ghi78
  admin delete abc123def456ghi78
  admin download abc123def456ghi78 ./output.enc
  admin list 50 0
  admin stats
  admin reports abc123def456ghi78
  admin cleanup
`
	fmt.Println(usage)
}

func viewFile(ctx context.Context, db *storage.Postgres, fs *storage.Filesystem, discord *services.Discord, fileID string) {
	file, err := db.GetFileForAdmin(ctx, fileID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting file: %v\n", err)
		os.Exit(1)
	}

	existsOnDisk := fs.FileExists(fileID)
	var sizeOnDisk int64
	if existsOnDisk {
		sizeOnDisk, _ = fs.GetFileSize(fileID)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("FILE DETAILS")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("ID:            %s\n", file.ID)
	fmt.Printf("Numeric Code:  %s\n", file.NumericCode)
	fmt.Printf("Original Name: %s\n", file.OriginalName)
	fmt.Printf("Size (DB):     %s (%d bytes)\n", formatFileSize(file.SizeBytes), file.SizeBytes)
	fmt.Printf("Size (Disk):   %s (%d bytes)\n", formatFileSize(sizeOnDisk), sizeOnDisk)
	fmt.Printf("Exists:        %t\n", existsOnDisk)
	fmt.Printf("Uploader IP:   %s\n", file.UploaderIP)
	fmt.Printf("Created:       %s\n", file.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Expires:       %s\n", file.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("Report Count:  %d\n", file.ReportCount)
	fmt.Printf("Is Deleted:    %t\n", file.IsDeleted)
	fmt.Println(strings.Repeat("=", 60))

	 
	if err := discord.SendAdminFileNotification(file, "view"); err != nil {
		fmt.Printf("Warning: Failed to send Discord notification: %v\n", err)
	}
}

func deleteFile(ctx context.Context, db *storage.Postgres, fs *storage.Filesystem, discord *services.Discord, fileID string) {
	 
	file, err := db.GetFileForAdmin(ctx, fileID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting file: %v\n", err)
		os.Exit(1)
	}

	 
	if err := db.MarkFileDeleted(ctx, fileID); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking file as deleted: %v\n", err)
		os.Exit(1)
	}

	 
	if err := fs.DeleteFile(fileID); err != nil {
		fmt.Printf("Warning: Error deleting file from disk: %v\n", err)
	}

	fmt.Printf("File %s has been deleted\n", fileID)

	 
	if err := discord.SendAdminFileNotification(file, "delete"); err != nil {
		fmt.Printf("Warning: Failed to send Discord notification: %v\n", err)
	}
}

func downloadFile(ctx context.Context, db *storage.Postgres, fs *storage.Filesystem, discord *services.Discord, fileID, outputPath string) {
	 
	file, err := db.GetFileForAdmin(ctx, fileID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting file: %v\n", err)
		os.Exit(1)
	}

	 
	if !fs.FileExists(fileID) {
		fmt.Fprintf(os.Stderr, "Error: File does not exist on disk\n")
		os.Exit(1)
	}

	 
	if outputPath == "" {
		outputPath = fmt.Sprintf("%s.enc", fileID)
	}

	 
	reader, err := fs.GetFileReader(fileID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer reader.Close()

	 
	outFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer outFile.Close()

	 
	written, err := outFile.ReadFrom(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Downloaded %s (%d bytes) to %s\n", fileID, written, outputPath)
	fmt.Printf("Original filename: %s\n", file.OriginalName)
	fmt.Println("Note: File is encrypted. You need the decryption password to view contents.")

	 
	if err := discord.SendAdminFileNotification(file, "download"); err != nil {
		fmt.Printf("Warning: Failed to send Discord notification: %v\n", err)
	}
}

func listFiles(ctx context.Context, db *storage.Postgres, limit, offset int) {
	files, err := db.GetAllFiles(ctx, limit, offset)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing files: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Println("No files found")
		return
	}

	fmt.Println(strings.Repeat("=", 120))
	fmt.Printf("%-20s %-14s %-30s %-12s %-8s %-8s\n", "ID", "CODE", "NAME", "SIZE", "REPORTS", "DELETED")
	fmt.Println(strings.Repeat("-", 120))

	for _, file := range files {
		name := file.OriginalName
		if len(name) > 28 {
			name = name[:25] + "..."
		}
		fmt.Printf("%-20s %-14s %-30s %-12s %-8d %-8t\n",
			file.ID,
			file.NumericCode,
			name,
			formatFileSize(file.SizeBytes),
			file.ReportCount,
			file.IsDeleted,
		)
	}

	fmt.Println(strings.Repeat("=", 120))
	fmt.Printf("Showing %d files (offset: %d)\n", len(files), offset)
}

func showStats(ctx context.Context, db *storage.Postgres, fs *storage.Filesystem) {
	totalFiles, activeFiles, totalReports, totalSizeDB, err := db.GetStats(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting stats: %v\n", err)
		os.Exit(1)
	}

	totalSizeDisk, err := fs.GetTotalSize()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting disk stats: %v\n", err)
		os.Exit(1)
	}

	diskFiles, _ := fs.GetAllFileIDs()
	sessionDirs, _ := fs.GetAllSessionIDs()

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("SYSTEM STATISTICS")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Total Files (DB):     %d\n", totalFiles)
	fmt.Printf("Active Files (DB):    %d\n", activeFiles)
	fmt.Printf("Total Reports:        %d\n", totalReports)
	fmt.Printf("Total Size (DB):      %s\n", formatFileSize(totalSizeDB))
	fmt.Printf("Total Size (Disk):    %s\n", formatFileSize(totalSizeDisk))
	fmt.Printf("Files on Disk:        %d\n", len(diskFiles))
	fmt.Printf("Active Upload Sessions: %d\n", len(sessionDirs))
	fmt.Println(strings.Repeat("=", 60))
}

func showReports(ctx context.Context, db *storage.Postgres, fileID string) {
	reports, err := db.GetReportsByFileID(ctx, fileID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting reports: %v\n", err)
		os.Exit(1)
	}

	if len(reports) == 0 {
		fmt.Printf("No reports found for file %s\n", fileID)
		return
	}

	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("REPORTS FOR FILE: %s\n", fileID)
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("%-6s %-20s %-30s\n", "ID", "REPORTER IP", "TIME")
	fmt.Println(strings.Repeat("-", 80))

	for _, report := range reports {
		fmt.Printf("%-6d %-20s %-30s\n",
			report.ID,
			report.ReporterIP,
			report.CreatedAt.Format(time.RFC3339),
		)
	}

	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("Total reports: %d\n", len(reports))
}

func forceCleanup(ctx context.Context, cfg *config.Config, db *storage.Postgres, fs *storage.Filesystem) {
	fmt.Println("Starting forced cleanup...")

	 
	expiredCount, err := db.DeleteExpiredFiles(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting expired files: %v\n", err)
	} else {
		fmt.Printf("Marked %d expired files as deleted\n", expiredCount)
	}

	 
	files, err := db.GetAllFiles(ctx, 1000, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting files: %v\n", err)
		os.Exit(1)
	}

	deletedBlobs := 0
	for _, file := range files {
		if file.IsDeleted && fs.FileExists(file.ID) {
			if err := fs.DeleteFile(file.ID); err != nil {
				fmt.Printf("Warning: Error deleting blob %s: %v\n", file.ID, err)
			} else {
				deletedBlobs++
			}
		}
	}
	fmt.Printf("Deleted %d file blobs\n", deletedBlobs)

	 
	orphanedCount, err := fs.CleanupOrphanedChunks(make(map[string]bool))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error cleaning orphaned chunks: %v\n", err)
	} else {
		fmt.Printf("Cleaned %d orphaned chunk directories\n", orphanedCount)
	}

	fmt.Println("Cleanup completed")
}

func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}