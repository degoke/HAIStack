package postgresdemo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/postgres"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Open returns a Postgres DSN and a cleanup function. It uses TEST_POSTGRES_DSN
// when set, otherwise it starts a disposable postgres:16-alpine container.
func Open(ctx context.Context, databaseName string) (string, func() error, error) {
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db, err := postgres.Open(ctx, dsn)
		if err != nil {
			return "", nil, fmt.Errorf("open TEST_POSTGRES_DSN: %w", err)
		}
		if err := db.Migrate(ctx); err != nil {
			db.Close()
			return "", nil, fmt.Errorf("migrate TEST_POSTGRES_DSN: %w", err)
		}
		return dsn, func() error {
			db.Close()
			return nil
		}, nil
	}

	if !dockerAvailable() {
		return "", nil, fmt.Errorf("docker unavailable and TEST_POSTGRES_DSN is not set")
	}

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase(databaseName),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("start postgres container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return "", nil, fmt.Errorf("postgres connection string: %w", err)
	}

	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		return "", nil, fmt.Errorf("open container postgres: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		_ = container.Terminate(ctx)
		return "", nil, fmt.Errorf("migrate container postgres: %w", err)
	}
	db.Close()

	return dsn, func() error {
		return container.Terminate(ctx)
	}, nil
}

func dockerAvailable() bool {
	if os.Getenv("DOCKER_HOST") == "" {
		out, err := exec.Command("docker", "context", "inspect", "-f", "{{.Endpoints.docker.Host}}").Output()
		if err == nil {
			if host := strings.TrimSpace(string(out)); host != "" {
				_ = os.Setenv("DOCKER_HOST", host)
			}
		}
	}
	checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(checkCtx, "docker", "info").Run() == nil
}
