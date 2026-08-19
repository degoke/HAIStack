package http

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const fhirXMLNamespace = "http://hl7.org/fhir"

// marshalFHIRXML provides a schema-neutral FHIR XML representation for the
// generic JSON resource model used by this package. FHIR XML represents
// primitives with a value attribute, complex values as nested elements, and
// repeated values as repeated elements.
func marshalFHIRXML(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode FHIR JSON for XML: %w", err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("FHIR resource must be a JSON object")
	}
	resourceType, ok := object["resourceType"].(string)
	if !ok || strings.TrimSpace(resourceType) == "" {
		return nil, fmt.Errorf("FHIR resourceType is required for XML")
	}

	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	start := xml.StartElement{
		Name: xml.Name{Local: resourceType},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: fhirXMLNamespace}},
	}
	if err := encodeFHIRObject(encoder, start, object, true); err != nil {
		return nil, err
	}
	if err := encoder.Flush(); err != nil {
		return nil, fmt.Errorf("encode FHIR XML: %w", err)
	}
	return out.Bytes(), nil
}

func encodeFHIRObject(encoder *xml.Encoder, start xml.StartElement, object map[string]any, root bool) error {
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := object[key]
		if key == "resourceType" || value == nil {
			continue
		}
		if err := encodeFHIRField(encoder, key, value); err != nil {
			return err
		}
	}
	if err := encoder.EncodeToken(start.End()); err != nil {
		return err
	}
	return nil
}

func encodeFHIRField(encoder *xml.Encoder, name string, value any) error {
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if item == nil {
				continue
			}
			if err := encodeFHIRField(encoder, name, item); err != nil {
				return err
			}
		}
		return nil
	}

	object, isObject := value.(map[string]any)
	if isObject {
		// Bundle.entry.resource carries a nested FHIR resource whose XML name is
		// its resourceType, rather than a resourceType attribute.
		if nestedType, ok := object["resourceType"].(string); ok && name == "resource" {
			resourceStart := xml.StartElement{Name: xml.Name{Local: name}}
			if err := encoder.EncodeToken(resourceStart); err != nil {
				return err
			}
			nestedStart := xml.StartElement{Name: xml.Name{Local: nestedType}}
			if err := encodeFHIRObject(encoder, nestedStart, object, false); err != nil {
				return err
			}
			return encoder.EncodeToken(resourceStart.End())
		}
		return encodeFHIRObject(encoder, xml.StartElement{Name: xml.Name{Local: name}}, object, false)
	}

	start := xml.StartElement{Name: xml.Name{Local: name}}
	text := primitiveXMLValue(value)
	start.Attr = []xml.Attr{{Name: xml.Name{Local: "value"}, Value: text}}
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	return encoder.EncodeToken(start.End())
}

func primitiveXMLValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32)
	default:
		return fmt.Sprint(typed)
	}
}

type fhirXMLNode struct {
	Name     string
	Attrs    map[string]string
	Children []*fhirXMLNode
	Text     string
}

// parseFHIRXML converts the common FHIR XML shape back into the generic JSON
// representation. It intentionally preserves unknown fields so custom FHIR
// resources and extensions can pass through the generic HTTP adapter.
func parseFHIRXML(data []byte) ([]byte, string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *fhirXMLNode
	for {
		token, err := decoder.Token()
		if err != nil {
			if root != nil && errors.Is(err, io.EOF) {
				break
			}
			return nil, "", fmt.Errorf("decode FHIR XML: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if root != nil {
				return nil, "", fmt.Errorf("multiple XML roots")
			}
			root, err = decodeFHIRXMLNode(decoder, typed)
			if err != nil {
				return nil, "", err
			}
		}
	}
	if root == nil {
		return nil, "", fmt.Errorf("FHIR XML document is empty")
	}
	object := fhirXMLNodeObject(root, true)
	data, err := json.Marshal(object)
	if err != nil {
		return nil, "", fmt.Errorf("encode FHIR JSON from XML: %w", err)
	}
	return data, root.Name, nil
}

func decodeFHIRXMLNode(decoder *xml.Decoder, start xml.StartElement) (*fhirXMLNode, error) {
	node := &fhirXMLNode{Name: start.Name.Local, Attrs: make(map[string]string)}
	for _, attr := range start.Attr {
		if attr.Name.Local != "xmlns" {
			node.Attrs[attr.Name.Local] = attr.Value
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode <%s>: %w", start.Name.Local, err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			child, err := decodeFHIRXMLNode(decoder, typed)
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, child)
		case xml.CharData:
			node.Text += string(typed)
		case xml.EndElement:
			if typed.Name == start.Name {
				return node, nil
			}
		}
	}
}

func fhirXMLNodeObject(node *fhirXMLNode, root bool) map[string]any {
	if !root && len(node.Children) == 0 {
		return map[string]any{"value": xmlPrimitive(node.Name, node.Attrs["value"])}
	}
	object := make(map[string]any)
	if root {
		object["resourceType"] = node.Name
	}
	if value, ok := node.Attrs["value"]; ok {
		object["value"] = xmlPrimitive(node.Name, value)
	}
	for _, child := range node.Children {
		var value any
		if child.Name == "resource" && len(child.Children) == 1 && child.Children[0].Name != "" {
			nested := fhirXMLNodeObject(child.Children[0], true)
			value = nested
		} else if len(child.Children) == 0 {
			if raw, ok := child.Attrs["value"]; ok {
				value = xmlPrimitive(child.Name, raw)
			} else {
				value = strings.TrimSpace(child.Text)
			}
		} else {
			value = fhirXMLNodeObject(child, false)
		}
		if existing, ok := object[child.Name]; ok {
			switch values := existing.(type) {
			case []any:
				object[child.Name] = append(values, value)
			default:
				object[child.Name] = []any{values, value}
			}
		} else {
			object[child.Name] = value
		}
	}
	return object
}

func xmlPrimitive(name, value string) any {
	// FHIR ids are strings even when their contents look like a boolean or
	// number (for example, "true" is a valid id value).
	if name == "id" {
		return value
	}
	if xmlBooleanField(name) {
		switch strings.ToLower(value) {
		case "true":
			return true
		case "false":
			return false
		}
	}
	if numericXMLField(name) {
		if integer, err := strconv.ParseInt(value, 10, 64); err == nil {
			return integer
		}
		if decimal, err := strconv.ParseFloat(value, 64); err == nil {
			return decimal
		}
	}
	return value
}

func numericXMLField(name string) bool {
	if name == "total" || name == "count" || name == "offset" || name == "min" || name == "max" {
		return true
	}
	return strings.HasPrefix(name, "value") && (strings.HasSuffix(name, "Integer") || strings.HasSuffix(name, "Decimal") || strings.HasSuffix(name, "PositiveInt") || strings.HasSuffix(name, "UnsignedInt"))
}

func xmlBooleanField(name string) bool {
	switch name {
	case "active", "deceasedBoolean", "multipleBirthBoolean", "isModifier", "isSummary", "mustSupport", "modifier", "primary":
		return true
	default:
		return strings.HasSuffix(name, "Boolean")
	}
}
