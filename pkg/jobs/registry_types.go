package jobs

const (
	// TypePrefixRegistry scopes registry and conformance background jobs.
	TypePrefixRegistry = "registry."

	// TypeRegistryPackageInstall installs a FHIR NPM package asynchronously.
	TypeRegistryPackageInstall = TypePrefixRegistry + "package_install"

	// TypeIGInstall installs an Implementation Guide package asynchronously.
	TypeIGInstall = TypePrefixRegistry + "ig.install"

	// TypeTerminologyInstall installs a terminology pack asynchronously.
	TypeTerminologyInstall = TypePrefixRegistry + "terminology.install"

	// TypePackageFetch fetches a package archive before install.
	TypePackageFetch = TypePrefixRegistry + "package.fetch"
)

// TerminologyInstallPayload is the job payload for registry.terminology.install.
type TerminologyInstallPayload struct {
	ScopeID string `json:"scopeId,omitempty"`
}

// PackageInstallPayload is the job payload for registry.package_install.
type PackageInstallPayload struct {
	Source    string `json:"source"`
	PackageID string `json:"packageId,omitempty"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
}
