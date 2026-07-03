package search

// fieldKeyForSort maps a FHIR _sort code to a typed index field key.
func fieldKeyForSort(code string) string {
	switch code {
	case "_id":
		return "token._id"
	case "_lastUpdated":
		return "date._lastUpdated"
	default:
		return ""
	}
}
