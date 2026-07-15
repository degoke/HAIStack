package appkit

import "fmt"

// PatientJSON returns a small Patient resource payload for examples.
func PatientJSON(given, family, phone string) []byte {
	return []byte(fmt.Sprintf(`{
  "resourceType": "Patient",
  "identifier": [
    {
      "system": "http://example.org/mrn",
      "value": "%s-%s"
    }
  ],
  "name": [
    {
      "family": %q,
      "given": [%q]
    }
  ],
  "telecom": [
    {
      "system": "phone",
      "value": %q
    }
  ],
  "gender": "female"
}`, family, given, family, given, phone))
}
