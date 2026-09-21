package semanticconversion

import (
	"encoding/json"
	"fmt"
)

// Category classifies a conversion pair.
const (
	CategoryUnchanged       = "unchanged"
	CategoryRemoved         = "removed"
	CategoryRenamed         = "renamed"
	CategoryCardinality     = "cardinality"
	CategoryTypeChange      = "type-change"
	CategoryCodeableConcept = "codeableconcept"
)

// Assertion is a path check on R4 and/or R5 instances.
// R4 expressions are evaluated with pkg/fhirpath when the R4 protobuf codec
// can load the instance. R5 expressions always use the JSON-path subset
// because HAIStack has no R5 codec yet.
type Assertion struct {
	Name string   `json:"name"`
	R4   string   `json:"r4,omitempty"`
	R5   string   `json:"r5,omitempty"`
	Want []string `json:"want,omitempty"`
}

// Pair is one R4 input and authored expected R5 output.
// Expected R5 is the published gold oracle in testdata/corpus.json.
type Pair struct {
	ID              string          `json:"id"`
	ResourceType    string          `json:"resourceType"`
	Category        string          `json:"category"`
	R4              json.RawMessage `json:"r4"`
	R5              json.RawMessage `json:"r5"`
	Assertions      []Assertion     `json:"assertions"`
	InformationLoss []string        `json:"informationLoss,omitempty"`
	Spec            string          `json:"spec,omitempty"`
}

// LoadCorpus returns the published gold pairs from testdata/corpus.json.
func LoadCorpus() ([]Pair, error) {
	return ParseCorpus(corpusJSON)
}

// ParseCorpus unmarshals a published corpus file.
func ParseCorpus(raw []byte) ([]Pair, error) {
	var pairs []Pair
	if err := json.Unmarshal(raw, &pairs); err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("semantic-conversion: corpus is empty")
	}
	seen := map[string]struct{}{}
	for i, p := range pairs {
		if p.ID == "" {
			return nil, fmt.Errorf("semantic-conversion: pair %d missing id", i)
		}
		if _, ok := seen[p.ID]; ok {
			return nil, fmt.Errorf("semantic-conversion: duplicate id %q", p.ID)
		}
		seen[p.ID] = struct{}{}
		if len(p.R4) == 0 || len(p.R5) == 0 {
			return nil, fmt.Errorf("semantic-conversion: pair %s missing r4 or r5 gold", p.ID)
		}
	}
	return pairs, nil
}
