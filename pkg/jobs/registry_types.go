package jobs

const (
	// TypePrefixRegistry scopes registry and conformance background jobs.
	TypePrefixRegistry = "registry."

	// TypeRegistryPackageInstall installs a FHIR NPM package asynchronously.
	TypeRegistryPackageInstall = TypePrefixRegistry + "package_install"
)

// PackageInstallPayload is the job payload for registry.package_install.
type PackageInstallPayload struct {
	Source    string `json:"source"`
	PackageID string `json:"packageId,omitempty"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
}
