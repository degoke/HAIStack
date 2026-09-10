package structuremap

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/sdc"
)

// Config wires StructureMap extraction into pkg/sdc.
type Config struct {
	Resolver Resolver
	Engine   Engine
}

// NewExtractor returns a StructureMapExtractor backed by this runtime.
func NewExtractor(cfg Config) sdc.StructureMapExtractor {
	return sdc.StructureMapExtractor{Run: ExtractorRun(cfg)}
}

// ExtractorRun builds the Run hook used by sdc.StructureMapExtractor.
func ExtractorRun(cfg Config) func(context.Context, sdc.Questionnaire, sdc.QuestionnaireResponse) ([]json.RawMessage, error) {
	return func(ctx context.Context, q sdc.Questionnaire, r sdc.QuestionnaireResponse) ([]json.RawMessage, error) {
		if cfg.Resolver == nil {
			return nil, fmt.Errorf("StructureMap resolver is unavailable")
		}
		if q.SourceStructureMap == "" {
			return nil, fmt.Errorf("questionnaire has no sourceStructureMap")
		}
		m, err := cfg.Resolver.Resolve(ctx, q.SourceStructureMap)
		if err != nil {
			return nil, err
		}
		qJSON, err := json.Marshal(q)
		if err != nil {
			return nil, fmt.Errorf("encode questionnaire: %w", err)
		}
		rJSON, err := json.Marshal(r)
		if err != nil {
			return nil, fmt.Errorf("encode questionnaire response: %w", err)
		}
		var qMap, rMap map[string]any
		if err := json.Unmarshal(qJSON, &qMap); err != nil {
			return nil, fmt.Errorf("decode questionnaire: %w", err)
		}
		if err := json.Unmarshal(rJSON, &rMap); err != nil {
			return nil, fmt.Errorf("decode questionnaire response: %w", err)
		}
		inputs := ExecuteInput{
			"questionnaire": qMap,
			"questionnaireResponse": rMap,
			"src":                   rMap,
			"q":                     qMap,
		}
		return cfg.Engine.Execute(ctx, m, inputs)
	}
}
