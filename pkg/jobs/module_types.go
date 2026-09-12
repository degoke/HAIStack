package jobs

const (
	// TypePrefixModules scopes module lifecycle background jobs.
	TypePrefixModules = "modules."

	// TypeModuleInstall installs a local module directory asynchronously.
	TypeModuleInstall = TypePrefixModules + "install"
)

// ModuleInstallPayload is the job payload for modules.install.
type ModuleInstallPayload struct {
	Path        string `json:"path"`
	UpgradeOnly bool   `json:"upgradeOnly,omitempty"`
}
