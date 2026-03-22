package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"

	_ "modernc.org/sqlite"

	"github.com/sysop/notebridge/internal/blob"
	"github.com/sysop/notebridge/internal/sync"
	"github.com/sysop/notebridge/internal/syncdb"
)

func main() {
	spcDSN := flag.String("spc-dsn", "", "SPC MariaDB DSN (user:pass@tcp(host:port)/db)")
	spcPath := flag.String("spc-path", "/mnt/supernote/supernote_data", "SPC file storage path")
	nbDBPath := flag.String("nb-db", "/data/notebridge/notebridge.db", "NoteBridge SQLite path")
	nbStoragePath := flag.String("nb-storage", "/data/notebridge/storage", "NoteBridge blob storage path")
	dryRun := flag.Bool("dry-run", false, "Show what would be migrated without writing")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Validate flags
	if *spcDSN == "" {
		fmt.Fprintf(os.Stderr, "error: -spc-dsn is required\n")
		os.Exit(1)
	}

	// Setup logging
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger := slog.New(handler)

	ctx := context.Background()

	// Connect to SPC MariaDB
	logger.Info("Connecting to SPC MariaDB", "dsn", maskDSN(*spcDSN))
	spcReader, err := NewSPCReader(*spcDSN)
	if err != nil {
		logger.Error("failed to connect to SPC", "error", err)
		os.Exit(1)
	}
	defer spcReader.Close()

	// Open NoteBridge SQLite database
	logger.Info("Opening NoteBridge SQLite database", "path", *nbDBPath)
	nbDB, err := sql.Open("sqlite", *nbDBPath)
	if err != nil {
		logger.Error("failed to open sqlite database", "error", err)
		os.Exit(1)
	}
	defer nbDB.Close()

	// Create NoteBridge store
	store := syncdb.NewStore(nbDB)

	// Create blob store
	logger.Info("Creating blob storage", "path", *nbStoragePath)
	blobStore := blob.NewLocalStore(*nbStoragePath)

	// Create Snowflake generator
	sf := sync.NewSnowflakeGenerator()

	// Create migrator
	migrator := NewMigrator(spcReader, store, blobStore, sf, *spcPath, logger)
	migrator.SetDryRun(*dryRun)

	if *dryRun {
		logger.Info("DRY RUN MODE - no data will be written")
	}

	// Run migration
	if err := migrator.Run(ctx); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}

	logger.Info("migration successful")
}

// maskDSN masks the password in a DSN for logging.
func maskDSN(dsn string) string {
	// Simple masking: find @ and show only the part after it
	for i, c := range dsn {
		if c == '@' {
			return "***:***@" + dsn[i+1:]
		}
	}
	return dsn
}
