// Package command implements haistack CLI subcommands using github.com/spf13/cobra.
//
// NewRootCommand constructs the full command tree and persistent flags shared
// by all subcommands. Options carries flag values; loadConfig merges them with
// internal/config before each command runs.
//
// # Command tree
//
//   - init — write starter haistack.yaml and .haistack/
//   - serve — start managed HTTP runtime
//   - validate — structural FHIR validation
//   - import — create or update one JSON resource file with an explicit conflict policy
//   - read, delete, export — resource inspection and file-safe data operations
//   - search — FHIR search with key=value parameters
//   - fhirpath eval — evaluate a FHIRPath expression
//   - sync push, sync pull, sync status — device synchronization
//   - module install, upgrade, plan, list, inspect, uninstall — module lifecycle
//   - config show, config validate — resolved configuration inspection
//   - audit list — persisted audit inspection
//   - reindex — synchronous search index rebuild
//
// # Persistent flags
//
// All commands inherit --config, --output, storage overrides, runtime overrides,
// and sync overrides from the root command. Command-specific flags (for example
// init --force) are registered on individual subcommands. The root command also
// exposes --version from Version (default "dev").
//
// # Error handling
//
// The root command sets SilenceUsage and SilenceErrors so handlers control
// error presentation via internal/app.Printer. Validation failures return a
// non-zero exit without printing usage text.
//
// main.go calls NewRootCommand().Execute() and exits with status 1 on error.
package command
