package cql

import (
	"strings"

	"github.com/iimos/ucum"
)

// UCUMConverter parses UCUM units and converts quantities between compatible units.
type UCUMConverter interface {
	Canonicalize(unit string) (string, bool)
	Convert(value float64, fromUnit, toUnit string) (float64, bool)
	Dimension(unit string) (string, bool)
}

// DefaultUCUMConverter uses github.com/iimos/ucum with CQL-friendly unit aliases.
func DefaultUCUMConverter() UCUMConverter {
	return defaultUCUM
}

var defaultUCUM UCUMConverter = &ucumEngine{conv: ucum.NewConverter()}

type ucumEngine struct {
	conv *ucum.Conv
}

func (e *ucumEngine) Canonicalize(unit string) (string, bool) {
	u, ok := e.prepareUnit(unit)
	if !ok {
		return "", false
	}
	parsed, err := ucum.Parse([]byte(u))
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}

func (e *ucumEngine) Convert(value float64, fromUnit, toUnit string) (float64, bool) {
	from, okFrom := e.prepareUnit(fromUnit)
	to, okTo := e.prepareUnit(toUnit)
	if !okFrom || !okTo {
		return 0, false
	}
	out, err := e.conv.ConvFloat64(value, from, to)
	if err != nil {
		return 0, false
	}
	return out, true
}

func (e *ucumEngine) Dimension(unit string) (string, bool) {
	u, ok := e.prepareUnit(unit)
	if !ok {
		return "", false
	}
	for _, base := range ucumCanonicalBases {
		if _, err := e.conv.ConvFloat64(1, u, base); err == nil {
			return base, true
		}
	}
	return "", false
}

var ucumCanonicalBases = []string{"m", "g", "L", "s", "rad", "K", "mol", "cd", "1"}

func (e *ucumEngine) prepareUnit(unit string) (string, bool) {
	unit = strings.TrimSpace(unit)
	if unit == "" || unit == "1" {
		return "1", true
	}
	if mapped, ok := cqlUCUMAlias(unit); ok {
		unit = mapped
	}
	return unit, true
}

func cqlUCUMAlias(unit string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(unit))
	switch key {
	case "in", "inch", "inches":
		return "[in_i]", true
	case "ft", "foot", "feet":
		return "[ft_i]", true
	case "lb", "lbs", "pound", "pounds":
		return "[lb_av]", true
	case "oz", "ounce", "ounces":
		return "[oz_av]", true
	case "st", "stone", "stones", "[st_av]":
		return "[stone_av]", true
	case "mi", "mile", "miles":
		return "[mi_us]", true
	case "gm", "gram", "grams":
		return "g", true
	case "meter", "meters":
		return "m", true
	case "liter", "liters":
		return "L", true
	case "ml":
		return "mL", true
	case "ul", "µl", "microliter", "microliters":
		return "uL", true
	case "mcg":
		return "ug", true
	default:
		return unit, true
	}
}

func (e *Engine) ucum() UCUMConverter {
	if e == nil || e.ucumConverter == nil {
		return defaultUCUM
	}
	return e.ucumConverter
}

func quantitySIValue(q Quantity, conv UCUMConverter) (float64, bool) {
	if isDimensionlessUnit(q.Unit) {
		return q.Value, true
	}
	if isTimeUnit(q.Unit) {
		return toSeconds(q), true
	}
	dim, ok := conv.Dimension(q.Unit)
	if !ok {
		return 0, false
	}
	v, ok := conv.Convert(q.Value, q.Unit, dim)
	return v, ok
}

func quantitySameDimension(a, b string, conv UCUMConverter) bool {
	if sameUnit(a, b) {
		return true
	}
	if isTimeUnit(a) && isTimeUnit(b) {
		return true
	}
	da, oka := conv.Dimension(a)
	db, okb := conv.Dimension(b)
	return oka && okb && da == db
}
