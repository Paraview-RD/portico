// Package testdb gives tests a real PostgreSQL database.
//
// There is no in-process substitute worth using: the schema relies on
// CHECK constraints, foreign keys, and timestamptz, and a fake that does not
// enforce them would let exactly the bugs those exist to catch slip through.
// So tests run against the real thing.
//
// One container is started for the whole test binary and each test gets its
// own database inside it — starting a container per test would dominate the
// runtime.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	// Registers the "pgx" driver for the admin connections below.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	once     sync.Once
	adminDSN string
	setupErr error
	counter  int
	mu       sync.Mutex
)

// EnvDSN lets a developer point the tests at an already-running database
// instead of starting a container, which is much faster in a tight loop:
//
//	PORTICO_TEST_DB_DSN=postgres://portico:portico@localhost:5443/postgres?sslmode=disable go test ./...
const EnvDSN = "PORTICO_TEST_DB_DSN"

// DSN returns a connection string for a fresh, empty database. Migrations
// have not been applied; store.Open does that.
func DSN(t *testing.T) string {
	t.Helper()

	once.Do(start)
	if setupErr != nil {
		t.Skipf("no test database available: %v\n"+
			"Start Docker, or set %s to a running PostgreSQL instance.", setupErr, EnvDSN)
	}

	mu.Lock()
	counter++
	name := fmt.Sprintf("portico_test_%d_%d", os.Getpid(), counter)
	mu.Unlock()

	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("connect to test server: %v", err)
	}
	defer func() { _ = admin.Close() }()

	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", adminDSN)
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()
		// WITH (FORCE) disconnects anything still attached, so one leaked
		// connection cannot block cleanup and leave databases behind.
		_, _ = cleanup.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})

	return replaceDatabase(adminDSN, name)
}

func start() {
	if dsn := os.Getenv(EnvDSN); dsn != "" {
		adminDSN = dsn
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	c, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("portico"),
		postgres.WithPassword("portico"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		setupErr = err
		return
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		setupErr = err
		return
	}
	adminDSN = dsn
}

// replaceDatabase swaps the database name in a URL-form DSN.
func replaceDatabase(dsn, name string) string {
	// postgres://user:pass@host:port/dbname?params
	slash := -1
	for i := len(dsn) - 1; i >= 0; i-- {
		if dsn[i] == '/' {
			slash = i
			break
		}
	}
	if slash < 0 {
		return dsn
	}

	query := ""
	for i := slash; i < len(dsn); i++ {
		if dsn[i] == '?' {
			query = dsn[i:]
			break
		}
	}
	return dsn[:slash+1] + name + query
}
