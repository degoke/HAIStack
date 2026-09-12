package parquetfhir

import (
	"sort"
	"strings"
	"unicode"
)

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isAnnotationField(name string) bool {
	return strings.HasPrefix(name, "__") && strings.Contains(name[2:], "_")
}

func isPrimitiveWrapperField(name string) bool {
	return strings.HasPrefix(name, "_") && !strings.HasPrefix(name, "__")
}

func annotationStartField(fieldName string) string {
	return "__" + fieldName + "_start"
}

func annotationEndField(fieldName string) string {
	return "__" + fieldName + "_end"
}

func annotationNumericField(fieldName string) string {
	return "__" + fieldName + "_numeric"
}

func annotationCanonicalField(fieldName string) string {
	return "__" + fieldName + "_canonical"
}

func quantityValueNumericField() string {
	return "__value_numeric"
}
