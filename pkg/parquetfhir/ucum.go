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
	"cm": {
		canonicalCode: "m",
		canonicalUnit: "meter",
		convert: func(v *big.Rat) *big.Rat {
			return new(big.Rat).Quo(v, big.NewRat(100, 1))
		},
	},
	"m": {
		canonicalCode: "m",
		canonicalUnit: "meter",
		convert:       func(v *big.Rat) *big.Rat { return new(big.Rat).Set(v) },
	},
	"[in_i]": {
		canonicalCode: "m",
		canonicalUnit: "meter",
		convert: func(v *big.Rat) *big.Rat {
			return new(big.Rat).Mul(v, big.NewRat(254, 10000))
		},
	},
	"[ft_i]": {
		canonicalCode: "m",
		canonicalUnit: "meter",
		convert: func(v *big.Rat) *big.Rat {
			return new(big.Rat).Mul(v, big.NewRat(3048, 10000))
		},
	},
	"g": {
		canonicalCode: "kg",
		canonicalUnit: "kilogram",
		convert: func(v *big.Rat) *big.Rat {
			return new(big.Rat).Quo(v, big.NewRat(1000, 1))
		},
	},
	"kg": {
		canonicalCode: "kg",
		canonicalUnit: "kilogram",
		convert:       func(v *big.Rat) *big.Rat { return new(big.Rat).Set(v) },
	},
	"[lb_av]": {
		canonicalCode: "kg",
		canonicalUnit: "kilogram",
		convert: func(v *big.Rat) *big.Rat {
			return new(big.Rat).Mul(v, big.NewRat(45359237, 100000000))
		},
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
	case "cm":
		return "cm"
	case "m", "meter":
		return "m"
	case "in", "inch", "[in_i]":
		return "[in_i]"
	case "ft", "foot", "[ft_i]":
		return "[ft_i]"
	case "g":
		return "g"
	case "kg", "kilogram":
		return "kg"
	case "lb", "lbs", "[lb_av]":
		return "[lb_av]"
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

	if system != "" && system != ucumSystem {
		return nil, nil
	}
	conv, ok := ucumConversions[code]
	if !ok {
		return nil, nil
	}
	converted = conv.convert(rat)
	canonicalCode = conv.canonicalCode
	canonicalUnit = conv.canonicalUnit
	canonicalSystem = ucumSystem

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
