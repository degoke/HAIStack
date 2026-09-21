package benchmarks

import (
	"fmt"
	"math/rand/v2"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Size names used by the generator.
const (
	SizeSmall  = "small"
	SizeMedium = "medium"
	SizeLarge  = "large"
)

// SizeSpec is a seeded dataset scale.
type SizeSpec struct {
	Patients     int
	Observations int
}

// Sizes is the published scale table.
var Sizes = map[string]SizeSpec{
	SizeSmall:  {Patients: 10, Observations: 20},
	SizeMedium: {Patients: 50, Observations: 100},
	SizeLarge:  {Patients: 200, Observations: 400},
}

// DefaultSeed is the deterministic generator seed (issue 11).
const DefaultSeed int64 = 11

// Dataset is a synthetic FHIR cohort.
type Dataset struct {
	Size         string
	Seed         int64
	Patients     []*types.ResourceEnvelope
	Observations []*types.ResourceEnvelope
}

// Generate builds a deterministic synthetic dataset.
func Generate(seed int64, size string) (*Dataset, error) {
	spec, ok := Sizes[size]
	if !ok {
		return nil, fmt.Errorf("unknown size %q", size)
	}
	if seed == 0 {
		seed = DefaultSeed
	}
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)^0x9e3779b97f4a7c15))
	codec := types.NewJSONCodec()
	ds := &Dataset{Size: size, Seed: seed}
	for i := 0; i < spec.Patients; i++ {
		id := fmt.Sprintf("pat-%04d", i+1)
		gender := []string{"female", "male", "other", "unknown"}[i%4]
		raw := []byte(fmt.Sprintf(`{"resourceType":"Patient","id":%q,"name":[{"family":"Bench%04d","given":["P"]}],"gender":%q}`, id, i+1, gender))
		env, err := codec.ParseJSON("Patient", raw)
		if err != nil {
			return nil, err
		}
		ds.Patients = append(ds.Patients, env)
	}
	for i := 0; i < spec.Observations; i++ {
		id := fmt.Sprintf("obs-%04d", i+1)
		pat := ds.Patients[rng.IntN(len(ds.Patients))]
		status := "final"
		if i%5 == 0 {
			status = "preliminary"
		}
		raw := []byte(fmt.Sprintf(`{"resourceType":"Observation","id":%q,"status":%q,"code":{"text":"Hb","coding":[{"code":"718-7"}]},"subject":{"reference":"Patient/%s"},"valueQuantity":{"value":%d,"unit":"g/dL"}}`, id, status, pat.ID, 10+rng.IntN(8)))
		env, err := codec.ParseJSON("Observation", raw)
		if err != nil {
			return nil, err
		}
		ds.Observations = append(ds.Observations, env)
	}
	return ds, nil
}
