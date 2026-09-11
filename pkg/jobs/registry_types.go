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

	// TypeTerminologyPreExpand pre-expands finite ValueSet projections asynchronously.
	TypeTerminologyPreExpand = TypePrefixRegistry + "terminology.pre_expand_valuesets"

	// TypePackageFetch fetches a package archive before install.
	TypePackageFetch = TypePrefixRegistry + "package.fetch"
)

// TerminologyInstallPayload is the job payload for registry.terminology.install.
type TerminologyInstallPayload struct {
	ScopeID            string `json:"scopeId,omitempty"`
	PreExpandValueSets bool   `json:"preExpandValueSets,omitempty"`
}

// TerminologyPreExpandPayload is the job payload for registry.terminology.pre_expand_valuesets.
type TerminologyPreExpandPayload struct {
	ScopeID string   `json:"scopeId,omitempty"`
	URL     string   `json:"url,omitempty"`
	Version string   `json:"version,omitempty"`
	URLs    []string `json:"urls,omitempty"`
}

// PackageInstallPayload is the job payload for registry.package_install.
type PackageInstallPayload struct {
	Source    string `json:"source"`
	PackageID string `json:"packageId,omitempty"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
}
