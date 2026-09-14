package store

// FilterTerminologyInstalls returns rows matching the supplied filter fields.
// Empty filter fields are ignored.
func FilterTerminologyInstalls(rows []TerminologyInstallRecord, filter TerminologyInstallFilter) []TerminologyInstallRecord {
	var out []TerminologyInstallRecord
	for _, row := range rows {
		if filter.PackName != "" && row.PackName != filter.PackName {
			continue
		}
		if filter.ResourceType != "" && row.ResourceType != filter.ResourceType {
			continue
		}
		if filter.CanonicalURL != "" && row.CanonicalURL != filter.CanonicalURL {
			continue
		}
		if filter.Version != "" && row.Version != filter.Version {
			continue
		}
		out = append(out, row)
	}
	return out
}
