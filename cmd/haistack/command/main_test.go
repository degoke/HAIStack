package command_test

import (
	"os"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/postgrestest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	postgrestest.TerminateShared()
	os.Exit(code)
}
