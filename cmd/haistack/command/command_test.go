package command_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/cmd/haistack/command"
	"github.com/degoke/health-ai-stack/cmd/haistack/internal/config"
)

func runCLI(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	if dir != "" {
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("chdir: %v", err)
		}
	}
	cmd := command.NewRootCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func repoCoreModule(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "modules", "core", "module.json")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Join(dir, "modules", "core")
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("modules/core not found from test working directory")
	return ""
}

func writeConfig(t *testing.T, dir string, sqlitePath string, modulePath string) {
	t.Helper()
	content := `storage:
  driver: sqlite
  sqlitePath: ` + sqlitePath + `
runtime:
  httpAddr: 127.0.0.1:0
  enableSearch: true
  modulePaths:
    - ` + modulePath + `
sync:
  hubURL: ""
  nodeID: runtime-node
`
	if err := os.WriteFile(filepath.Join(dir, config.DefaultConfigFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestInitCreatesDefaultYAML(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	stdout, _, err := runCLI(t, dir, "init")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(stdout, "created haistack.yaml") {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, config.DefaultConfigFile)); err != nil {
		t.Fatalf("config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".haistack")); err != nil {
		t.Fatalf(".haistack missing: %v", err)
	}
}

func TestInitDoesNotOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	if _, _, err := runCLI(t, dir, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	_, _, err := runCLI(t, dir, "init")
	if err == nil {
		t.Fatal("expected error on second init without --force")
	}
}

func TestInitWithCustomConfigPathCreatesSiblingDataDir(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	target := filepath.Join(dir, "configs", "haistack.yaml")
	stdout, _, err := runCLI(t, dir, "init", "--config", target)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(stdout, "created "+target) {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "configs", ".haistack")); err != nil {
		t.Fatalf("custom data dir missing: %v", err)
	}
}

func TestImportUsesDefaultsWithoutConfigFileWhenFlagOverridesProvided(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	patientPath := filepath.Join(dir, "patient.json")
	if err := os.WriteFile(patientPath, []byte(`{"resourceType":"Patient","id":"flag-only-1","name":[{"family":"FlagOnly"}]}`), 0o644); err != nil {
		t.Fatalf("write patient: %v", err)
	}

	stdout, _, err := runCLI(t, dir,
		"import", patientPath,
		"--sqlite-path", filepath.Join(dir, "flags.db"),
		"--module-path", repoCoreModule(t),
		"--output", "json",
	)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(stdout, `"action": "create"`) {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestValidateReturnsNonZeroForInvalidResource(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "test.db"), coreModule)

	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{"resourceType":"Patient","id":"bad id!","name":[{"family":"Doe"}]}`), 0o644); err != nil {
		t.Fatalf("write bad resource: %v", err)
	}

	_, _, err := runCLI(t, dir, "validate", badPath)
	if err == nil {
		t.Fatal("expected validation failure")
	}
}

func TestImportCreatesThenUpdates(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "test.db"), coreModule)

	patientPath := filepath.Join(dir, "patient.json")
	patient := []byte(`{"resourceType":"Patient","id":"cli-pat-1","name":[{"family":"First"}]}`)
	if err := os.WriteFile(patientPath, patient, 0o644); err != nil {
		t.Fatalf("write patient: %v", err)
	}

	stdout, _, err := runCLI(t, dir, "import", patientPath, "--output", "json")
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &first); err != nil {
		t.Fatalf("decode first import: %v", err)
	}
	if first["action"] != "create" {
		t.Fatalf("first action = %v", first["action"])
	}

	updated := []byte(`{"resourceType":"Patient","id":"cli-pat-1","name":[{"family":"Updated"}]}`)
	if err := os.WriteFile(patientPath, updated, 0o644); err != nil {
		t.Fatalf("write updated patient: %v", err)
	}
	stdout, _, err = runCLI(t, dir, "import", patientPath, "--output", "json")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	var second map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &second); err != nil {
		t.Fatalf("decode second import: %v", err)
	}
	if second["action"] != "update" {
		t.Fatalf("second action = %v", second["action"])
	}
}

func TestSearchParsesKeyValueArgs(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "test.db"), coreModule)

	patientPath := filepath.Join(dir, "patient.json")
	if err := os.WriteFile(patientPath, []byte(`{"resourceType":"Patient","name":[{"family":"Searchable"}]}`), 0o644); err != nil {
		t.Fatalf("write patient: %v", err)
	}
	if _, _, err := runCLI(t, dir, "import", patientPath); err != nil {
		t.Fatalf("import: %v", err)
	}

	stdout, _, err := runCLI(t, dir, "search", "Patient", "name=Searchable")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(stdout, "Patient/") {
		t.Fatalf("search stdout = %q", stdout)
	}
}

func TestFHIRPathEvalJSON(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "test.db"), coreModule)

	patientPath := filepath.Join(dir, "patient.json")
	if err := os.WriteFile(patientPath, []byte(`{"resourceType":"Patient","active":true,"name":[{"family":"Doe"}]}`), 0o644); err != nil {
		t.Fatalf("write patient: %v", err)
	}

	stdout, _, err := runCLI(t, dir, "fhirpath", "eval", patientPath, "Patient.active", "--output", "json")
	if err != nil {
		t.Fatalf("fhirpath eval: %v", err)
	}
	if !strings.Contains(stdout, `"Boolean"`) || !strings.Contains(stdout, `true`) {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSyncStatusEmptyState(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()

	coreModule := repoCoreModule(t)
	writeConfig(t, dir, filepath.Join(dir, "test.db"), coreModule)

	stdout, _, err := runCLI(t, dir, "sync", "status", "--output", "json")
	if err != nil {
		t.Fatalf("sync status: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &report); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if report["pendingRetryPushJobs"].(float64) != 0 {
		t.Fatalf("pending jobs = %v", report["pendingRetryPushJobs"])
	}
}
