package postgrestest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/postgres"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const sharedDatabase = "haistack_test_shared"

var shared struct {
	sync.Mutex
	ready bool
	dsn   string
	term  func()
}

func initDockerHost() {
	if os.Getenv("DOCKER_HOST") != "" {
		return
	}
	out, err := exec.Command("docker", "context", "inspect", "-f", "{{.Endpoints.docker.Host}}").Output()
	if err != nil {
		return
	}
	if host := strings.TrimSpace(string(out)); host != "" {
		_ = os.Setenv("DOCKER_HOST", host)
	}
}

// DockerAvailable reports whether the Docker daemon is reachable.
func DockerAvailable() bool {
	initDockerHost()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}

// TerminateShared stops the shared testcontainer started by SharedDSN.
func TerminateShared() {
	shared.Lock()
	defer shared.Unlock()
	if shared.term != nil {
		shared.term()
		shared.term = nil
	}
	shared.ready = false
	shared.dsn = ""
}

// SharedDSN returns a migrated Postgres DSN reused across tests in one process.
func SharedDSN(t *testing.T) string {
	t.Helper()
	shared.Lock()
	defer shared.Unlock()

	if shared.ready {
		return shared.dsn
	}

	ctx := context.Background()
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db, err := postgres.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("Open TEST_POSTGRES_DSN: %v", err)
		}
		if err := db.Migrate(ctx); err != nil {
			db.Close()
			t.Fatalf("Migrate: %v", err)
		}
		db.Close()
		shared.dsn = dsn
		shared.ready = true
		return shared.dsn
	}

	if !DockerAvailable() {
		t.Skip("postgres unavailable: set TEST_POSTGRES_DSN or start Docker")
	}

	container, err := runContainer(ctx, sharedDatabase)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("connection string: %v", err)
	}

	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("Migrate: %v", err)
	}
	db.Close()

	shared.dsn = dsn
	shared.term = func() { _ = container.Terminate(ctx) }
	shared.ready = true
	return shared.dsn
}

// OpenDSN returns a migrated Postgres DSN for integration tests.
// Prefer SharedDSN when multiple tests in one package need Postgres.
func OpenDSN(t *testing.T, database string) (string, func()) {
	t.Helper()
	if database == sharedDatabase {
		dsn := SharedDSN(t)
		return dsn, func() {}
	}

	ctx := context.Background()
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db, err := postgres.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("Open TEST_POSTGRES_DSN: %v", err)
		}
		if err := db.Migrate(ctx); err != nil {
			db.Close()
			t.Fatalf("Migrate: %v", err)
		}
		return dsn, db.Close
	}

	if !DockerAvailable() {
		t.Skip("postgres unavailable: set TEST_POSTGRES_DSN or start Docker")
	}

	container, err := runContainer(ctx, database)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("connection string: %v", err)
	}

	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("Migrate: %v", err)
	}
	db.Close()

	return dsn, func() { _ = container.Terminate(ctx) }
}

// SharedDB opens a connection to the shared migrated Postgres instance.
func SharedDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := SharedDSN(t)
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open shared postgres: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func runContainer(ctx context.Context, database string) (*tcpostgres.PostgresContainer, error) {
	var (
		container *tcpostgres.PostgresContainer
		err       error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("%v", r)
			}
		}()
		container, err = tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase(database),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
		)
	}()
	return container, err
}
