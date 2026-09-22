package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/degoke/haistack/pkg/types"
	"github.com/degoke/haistack/research/internal/researchutil"
)

const datasetSeed = uint64(11)

// Size names for generated datasets.
const (
	SizeSmall  = "small"
	SizeMedium = "medium"
	SizeLarge  = "large"
)

// DatasetSize is the number of patients and observations for a named size.
type DatasetSize struct {
	Name         string
	Patients     int
	Observations int
}

func sizeFor(name string) DatasetSize {
	switch name {
	case SizeMedium:
		return DatasetSize{Name: SizeMedium, Patients: 100, Observations: 200}
	case SizeLarge:
		return DatasetSize{Name: SizeLarge, Patients: 400, Observations: 800}
	default:
		return DatasetSize{Name: SizeSmall, Patients: 20, Observations: 40}
	}
}

// Generate returns a seeded synthetic FHIR dataset. Seed 11 is fixed.
func Generate(size string) (patients, observations []*types.ResourceEnvelope, err error) {
	spec := sizeFor(size)
	rng := rand.New(rand.NewPCG(datasetSeed, datasetSeed))
	genders := []string{"female", "male", "other", "unknown"}
	codes := []struct{ Code, Display string }{
		{"8867-4", "Heart rate"},
		{"8480-6", "Systolic blood pressure"},
		{"8310-5", "Body temperature"},
		{"2708-6", "Oxygen saturation"},
	}
	for i := 0; i < spec.Patients; i++ {
		id := fmt.Sprintf("pat-%04d", i)
		env, err := researchutil.ParseResource("Patient", researchutil.MustJSON(map[string]any{
			"resourceType": "Patient",
			"id":           id,
			"gender":       genders[rng.IntN(len(genders))],
			"name": []any{map[string]any{
				"family": fmt.Sprintf("Family%04d", i),
				"given":  []any{fmt.Sprintf("Given%04d", i)},
			}},
		}))
		if err != nil {
			return nil, nil, err
		}
		patients = append(patients, env)
	}
	for i := 0; i < spec.Observations; i++ {
		pat := patients[i%len(patients)]
		code := codes[i%len(codes)]
		env, err := researchutil.ParseResource("Observation", researchutil.MustJSON(map[string]any{
			"resourceType": "Observation",
			"id":           fmt.Sprintf("obs-%04d", i),
			"status":       "final",
			"code": map[string]any{
				"text": code.Display,
				"coding": []any{map[string]any{
					"system": "http://loinc.org",
					"code":   code.Code,
				}},
			},
			"subject": map[string]any{"reference": "Patient/" + pat.ID},
			"valueQuantity": map[string]any{
				"value":  50 + rng.Float64()*80,
				"unit":   "1",
				"system": "http://unitsofmeasure.org",
			},
		}))
		if err != nil {
			return nil, nil, err
		}
		observations = append(observations, env)
	}
	return patients, observations, nil
}

// Dump writes the seeded synthetic Patient and Observation JSON into dir
// (resourceType/id.json plus manifest.json) for external adapter comparisons.
func Dump(dir, size string) error {
	if dir == "" {
		return fmt.Errorf("dump directory is required")
	}
	patients, observations, err := Generate(size)
	if err != nil {
		return err
	}
	spec := sizeFor(size)
	for _, env := range append(append([]*types.ResourceEnvelope{}, patients...), observations...) {
		sub := filepath.Join(dir, env.ResourceType)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		path := filepath.Join(sub, env.ID+".json")
		if err := os.WriteFile(path, env.JSON, 0o644); err != nil {
			return err
		}
	}
	manifest, err := json.MarshalIndent(map[string]any{
		"seed":         datasetSeed,
		"size":         spec.Name,
		"patients":     len(patients),
		"observations": len(observations),
		"synthetic":    true,
		"phi":          false,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(manifest, '\n'), 0o644)
}
