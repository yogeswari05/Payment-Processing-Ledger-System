package testutil

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/payflow/payflow/internal/database"
)

var (
	globalPool     *pgxpool.Pool
	globalCleanup  func()
	globalInitOnce sync.Once
	globalErr      error
)

// SetupTestDB initializes a real PostgreSQL connection pool for integration testing.
func SetupTestDB(t testing.TB) *pgxpool.Pool {
	t.Helper()

	globalInitOnce.Do(func() {
		globalPool, globalCleanup, globalErr = initDatabase()
	})

	if globalErr != nil {
		t.Fatalf("failed to initialize test database: %v", globalErr)
	}

	// Truncate tables for a clean slate before each test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	truncateQuery := `
		TRUNCATE TABLE
			reconciliation_findings,
			reconciliation_runs,
			consumed_events,
			outbox_events,
			idempotency_keys,
			ledger_entries,
			ledger_transactions,
			ledger_accounts,
			refunds,
			payments,
			customers,
			merchants
		CASCADE;
	`
	if _, err := globalPool.Exec(ctx, truncateQuery); err != nil {
		t.Fatalf("failed to truncate test database: %v", err)
	}

	return globalPool
}

func initDatabase() (*pgxpool.Pool, func(), error) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Case 1: Environment variable provided (e.g. CI / Docker)
	if dbURL := os.Getenv("TEST_DATABASE_URL"); dbURL != "" {
		pool, err := database.NewPool(ctx, database.Config{
			URL:      dbURL,
			MaxConns: 30,
			MinConns: 5,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed connecting to TEST_DATABASE_URL: %w", err)
		}
		if err := database.MigrateUp(ctx, pool, logger); err != nil {
			pool.Close()
			return nil, nil, fmt.Errorf("failed applying migrations: %w", err)
		}
		return pool, pool.Close, nil
	}

	// Case 2: Local ephemeral PostgreSQL cluster
	pgBinDir := findPostgresBinDir()
	if pgBinDir == "" {
		return nil, nil, fmt.Errorf("no PostgreSQL binary found and TEST_DATABASE_URL not set")
	}

	port := getFreePort()
	tempDir, err := os.MkdirTemp("", "payflow_pg_test_*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating temp dir for pg: %w", err)
	}

	initdbExe := filepath.Join(pgBinDir, "initdb")
	postgresExe := filepath.Join(pgBinDir, "postgres")
	if runtime.GOOS == "windows" {
		initdbExe += ".exe"
		postgresExe += ".exe"
	}

	// Run initdb
	initCmd := exec.Command(initdbExe, "-D", tempDir, "-U", "postgres", "-A", "trust", "--no-sync")
	if out, err := initCmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("initdb failed: %s: %w", string(out), err)
	}

	// Create logfile for postgres
	logPath := filepath.Join(tempDir, "pg.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("failed creating log file: %w", err)
	}

	// Start postgres server directly as a child process with non-blocking Start()
	pgCmd := exec.Command(
		postgresExe,
		"-D", tempDir,
		"-p", strconv.Itoa(port),
		"-h", "127.0.0.1",
		"-c", "fsync=off",
		"-c", "synchronous_commit=off",
		"-c", "full_page_writes=off",
	)
	pgCmd.Stdout = logFile
	pgCmd.Stderr = logFile

	if err := pgCmd.Start(); err != nil {
		_ = logFile.Close()
		_ = os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("failed starting postgres process: %w", err)
	}

	dbURL := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/postgres?sslmode=disable", port)

	// Poll until database server accepts connections
	var pool *pgxpool.Pool
	ready := false
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		pool, err = database.NewPool(ctx, database.Config{
			URL:      dbURL,
			MaxConns: 30,
			MinConns: 5,
		})
		if err == nil {
			ready = true
			break
		}
	}

	if !ready {
		_ = pgCmd.Process.Kill()
		_ = pgCmd.Wait()
		_ = logFile.Close()
		_ = os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("ephemeral postgres did not become ready in time: %w", err)
	}

	// Apply migrations
	if err := database.MigrateUp(ctx, pool, logger); err != nil {
		pool.Close()
		_ = pgCmd.Process.Kill()
		_ = pgCmd.Wait()
		_ = logFile.Close()
		_ = os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("failed running migrations on ephemeral pg: %w", err)
	}

	cleanup := func() {
		pool.Close()
		_ = pgCmd.Process.Kill()
		_ = pgCmd.Wait()
		_ = logFile.Close()
		_ = os.RemoveAll(tempDir)
	}

	return pool, cleanup, nil
}

func findPostgresBinDir() string {
	candidates := []string{
		`C:\Program Files\PostgreSQL\17\bin`,
		`C:\Program Files\PostgreSQL\16\bin`,
		`C:\Program Files\PostgreSQL\15\bin`,
		`/usr/lib/postgresql/17/bin`,
		`/usr/lib/postgresql/16/bin`,
		`/usr/local/bin`,
		`/usr/bin`,
	}
	for _, dir := range candidates {
		initdb := filepath.Join(dir, "initdb")
		if runtime.GOOS == "windows" {
			initdb += ".exe"
		}
		if _, err := os.Stat(initdb); err == nil {
			return dir
		}
	}
	if path, err := exec.LookPath("initdb"); err == nil {
		return filepath.Dir(path)
	}
	return ""
}

func getFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return 5433 + r.Intn(1000)
}

// NewTestLogger creates a quiet logger for tests.
func NewTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
