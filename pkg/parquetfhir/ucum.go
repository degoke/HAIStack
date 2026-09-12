package parquetfhir

import (
	"fmt"
	"math/big"
	"strings"
)

const ucumSystem = "http://unitsofmeasure.org"

type ucumConversion struct {
	canonicalCode string
	canonicalUnit string
	convert       func(value *big.Rat) *big.Rat
}

var ucumConversions = map[string]ucumConversion{
	"Cel": {
		canonicalCode: "K",
		canonicalUnit: "Kelvin",
		convert: func(v *big.Rat) *big.Rat {
			out := new(big.Rat).Set(v)
			out.Add(out, big.NewRat(27315, 100))
			return out
		},
	},
	"[degF]": {
		canonicalCode: "K",
		canonicalUnit: "Kelvin",
		convert: func(v *big.Rat) *big.Rat {
			// (F - 32) * 5/9 + 273.15
			out := new(big.Rat).Sub(v, big.NewRat(32, 1))
			out.Mul(out, big.NewRat(5, 9))
			out.Add(out, big.NewRat(27315, 100))
			return out
		},
	},
	"K": {
		canonicalCode: "K",
		canonicalUnit: "Kelvin",
		convert:       func(v *big.Rat) *big.Rat { return new(big.Rat).Set(v) },
	},
}

func normalizeUCUMCode(code, unit string) string {
	code = strings.TrimSpace(code)
	if code != "" {
		return code
	}
	switch strings.TrimSpace(unit) {
	case "C", "°C", "Cel":
		return "Cel"
	case "F", "°F":
		return "[degF]"
	case "K":
		return "K"
	default:
		return code
	}
}

func canonicalizeQuantity(qty map[string]any) (map[string]any, error) {
	if qty == nil {
		return nil, nil
	}
	rawValue, ok := qty["value"]
	if !ok || rawValue == nil {
		return nil, nil
	}
	decimalStr, err := coerceDecimalString(rawValue)
	if err != nil {
		return nil, fmt.Errorf("quantity value: %w", err)
	}
	rat, ok := new(big.Rat).SetString(decimalStr)
	if !ok {
		return nil, fmt.Errorf("invalid quantity value %q", decimalStr)
	}

	system := stringOrEmpty(qty["system"])
	code := normalizeUCUMCode(stringOrEmpty(qty["code"]), stringOrEmpty(qty["unit"]))
	unit := stringOrEmpty(qty["unit"])

	canonicalCode := code
	canonicalUnit := unit
	canonicalSystem := system
	converted := new(big.Rat).Set(rat)

	if system == "" || system == ucumSystem {
		if conv, ok := ucumConversions[code]; ok {
			converted = conv.convert(rat)
			canonicalCode = conv.canonicalCode
			canonicalUnit = conv.canonicalUnit
			canonicalSystem = ucumSystem
		}
	}

	canonicalValue := converted.FloatString(6)
	canonicalValue = strings.TrimRight(strings.TrimRight(canonicalValue, "0"), ".")
	numeric, err := decimalBytes(canonicalValue)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"value":                   canonicalValue,
		"code":                    canonicalCode,
		"system":                  canonicalSystem,
		"unit":                    canonicalUnit,
		quantityValueNumericField(): numeric,
	}, nil
}
