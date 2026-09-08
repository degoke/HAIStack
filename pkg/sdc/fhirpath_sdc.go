package sdc

import (
	"sync"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/shopspring/decimal"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
)

var sdcItemHolder sync.Mutex
var sdcCurrentItem *Item

// NewSDCFHIRPathEngine returns a FHIRPath engine with SDC custom functions such as weight().
func NewSDCFHIRPathEngine(cfg fhirpath.Config) (fhirpath.Engine, error) {
	if cfg.Functions == nil {
		cfg.Functions = map[string]fhirpath.Function{}
	}
	if cfg.FunctionArity == nil {
		cfg.FunctionArity = map[string]int{}
	}
	cfg.Functions["weight"] = sdcWeightFunction
	cfg.FunctionArity["weight"] = 1
	return fhirpath.NewEngine(cfg)
}

func withSDCFHIRPathItem(item *Item, fn func()) {
	sdcItemHolder.Lock()
	sdcCurrentItem = item
	defer func() {
		sdcCurrentItem = nil
		sdcItemHolder.Unlock()
	}()
	fn()
}

func sdcWeightFunction(input fhirpath.Collection, args ...fhirpath.Collection) (fhirpath.Collection, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	item := sdcCurrentItem
	if item == nil {
		return fhirpath.Collection{fhirpath.NewValue(system.Decimal(decimal.Zero))}, nil
	}
	target, ok := codingFrom(fhirPathScalar(args[0][0]))
	if !ok {
		return fhirpath.Collection{fhirpath.NewValue(system.Decimal(decimal.Zero))}, nil
	}
	for _, option := range item.AnswerOption {
		code, ok := answerOptionCoding(option.Value)
		if !ok || !Equal(code, target) {
			continue
		}
		if option.OptionWeight != nil {
			return fhirpath.Collection{fhirpath.NewValue(system.Decimal(decimal.NewFromFloat(*option.OptionWeight)))}, nil
		}
		return fhirpath.Collection{fhirpath.NewValue(system.Decimal(decimal.Zero))}, nil
	}
	return fhirpath.Collection{fhirpath.NewValue(system.Decimal(decimal.Zero))}, nil
}
