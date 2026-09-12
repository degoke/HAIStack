package parquetfhir

import (
	"strings"
	"strconv"

	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/parquet-go/parquet-go"
)

var fhirPrimitiveTypes = map[string]struct{}{
	"base64Binary": {}, "boolean": {}, "canonical": {}, "code": {}, "date": {},
	"dateTime": {}, "decimal": {}, "id": {}, "instant": {}, "integer": {},
	"integer64": {}, "markdown": {}, "oid": {}, "positiveInt": {}, "string": {},
	"time": {}, "unsignedInt": {}, "uri": {}, "url": {}, "uuid": {}, "xhtml": {},
}

func isPrimitiveFHIRType(typ string) bool {
	if typ == "" {
		return false
	}
	if strings.HasPrefix(typ, "http://") {
		return true
	}
	_, ok := fhirPrimitiveTypes[typ]
	return ok
}

func normalizeFHIRType(typ string) string {
	if strings.HasPrefix(typ, "http://") {
		return "string"
	}
	return typ
}

func primitiveNode(fhirType string) parquet.Node {
	switch normalizeFHIRType(fhirType) {
	case "boolean":
		return parquet.Leaf(parquet.BooleanType)
	case "integer":
		return parquet.Int(32)
	case "integer64":
		return parquet.Int(64)
	case "positiveInt", "unsignedInt":
		return parquet.Uint(32)
	case "decimal":
		return parquet.String()
	default:
		return parquet.String()
	}
}

func decimalAnnotationNode() parquet.Node {
	return parquet.Decimal(6, 38, parquet.FixedLenByteArrayType(16))
}

func timestampAnnotationNode() parquet.Node {
	return parquet.Timestamp(parquet.Millisecond)
}

func elementType(el *validate.ElementDefinition, fieldName string, choiceTypes map[string]string) string {
	if typ, ok := choiceTypes[fieldName]; ok {
		return typ
	}
	if el == nil {
		return "string"
	}
	if strings.Contains(el.Path, "[x]") {
		for _, typ := range el.Types {
			if choiceFieldName(choiceBaseName(el.Path), typ) == fieldName {
				return normalizeFHIRType(typ)
			}
		}
	}
	if len(el.Types) == 1 {
		return normalizeFHIRType(el.Types[0])
	}
	for _, typ := range el.Types {
		if isPrimitiveFHIRType(typ) {
			return normalizeFHIRType(typ)
		}
	}
	if len(el.Types) > 0 {
		return normalizeFHIRType(el.Types[0])
	}
	return "string"
}

func isRepeatingMax(max string) bool {
	if max == "" || max == "1" {
		return false
	}
	if max == "*" {
		return true
	}
	n, err := strconv.Atoi(max)
	return err == nil && n > 1
}

func isDateLikeType(fhirType string) bool {
	switch normalizeFHIRType(fhirType) {
	case "date", "dateTime", "instant":
		return true
	default:
		return false
	}
}

func isDecimalType(fhirType string) bool {
	return normalizeFHIRType(fhirType) == "decimal"
}
