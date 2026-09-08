package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/terminology"
)

const conformanceTerminologyScope = "conformance"

func loadTerminology(ctx context.Context, dir string) (terminology.Service, error) {
	st := terminology.NewMemoryStore()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &terminology.LocalService{Store: st, ScopeID: conformanceTerminologyScope}, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var peek struct {
			ResourceType string `json:"resourceType"`
		}
		if err := json.Unmarshal(data, &peek); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		switch peek.ResourceType {
		case "CodeSystem", "ValueSet":
			if err := terminology.Compile(ctx, st, conformanceTerminologyScope, "", data); err != nil {
				return nil, fmt.Errorf("compile %s: %w", entry.Name(), err)
			}
		}
	}
	return &terminology.LocalService{Store: st, ScopeID: conformanceTerminologyScope}, nil
}
