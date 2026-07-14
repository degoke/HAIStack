package binary

import (
	"encoding/json"
	"fmt"
)

const (
	extensionURLBlobRef = "https://health-ai-stack.dev/fhir/StructureDefinition/blob-reference"
	attachmentExtKey    = "_blobRef"
)

// BuildBinaryMetadataJSON returns metadata-only FHIR Binary JSON without a data element.
func BuildBinaryMetadataJSON(id, contentType string, ref BlobReference) ([]byte, error) {
	obj := map[string]interface{}{
		"resourceType": "Binary",
		"contentType":  contentType,
		"extension": []map[string]interface{}{
			{
				"url":         extensionURLBlobRef,
				"valueString": mustMarshalRef(ref),
			},
		},
	}
	if id != "" {
		obj["id"] = id
	}
	return json.Marshal(obj)
}

// BuildAttachmentMetadata builds DocumentReference.content[].attachment metadata without payload bytes.
func BuildAttachmentMetadata(contentType string, ref BlobReference) (map[string]interface{}, error) {
	attachment := map[string]interface{}{
		"contentType":    contentType,
		"size":           ref.Size,
		"hash":           ref.SHA256,
		"url":            fmt.Sprintf("blob:%s", ref.BlobID),
		attachmentExtKey: ref,
	}
	return attachment, nil
}

// EmbedDocumentAttachment embeds blob metadata into DocumentReference JSON at content[index].
func EmbedDocumentAttachment(docJSON []byte, contentIndex int, contentType string, ref BlobReference) ([]byte, error) {
	obj, err := decodeJSONObject(docJSON)
	if err != nil {
		return nil, err
	}
	content, err := contentArray(obj)
	if err != nil {
		return nil, err
	}
	if contentIndex < 0 || contentIndex >= len(content) {
		return nil, fmt.Errorf("%w: content index %d out of range", ErrInvalidArgument, contentIndex)
	}
	entry, ok := content[contentIndex].(map[string]interface{})
	if !ok {
		entry = map[string]interface{}{}
		content[contentIndex] = entry
	}
	attachment, err := BuildAttachmentMetadata(contentType, ref)
	if err != nil {
		return nil, err
	}
	entry["attachment"] = attachment
	obj["content"] = content
	return json.Marshal(obj)
}

// ExtractBlobReferences scans FHIR JSON for embedded blob references.
func ExtractBlobReferences(data []byte) ([]BlobReference, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	var refs []BlobReference
	collectBlobRefs(v, &refs)
	return refs, nil
}

// ExtractBinaryBlobRef returns the blob reference from a metadata-only Binary resource.
func ExtractBinaryBlobRef(data []byte) (*BlobReference, error) {
	obj, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	rt, _ := obj["resourceType"].(string)
	if rt != "Binary" {
		return nil, fmt.Errorf("%w: not a Binary resource", ErrInvalidArgument)
	}
	if _, hasData := obj["data"]; hasData {
		return nil, fmt.Errorf("%w: Binary resource contains inline data", ErrInvalidArgument)
	}
	exts, ok := obj["extension"].([]interface{})
	if !ok {
		return nil, ErrNotFound
	}
	for _, ext := range exts {
		m, ok := ext.(map[string]interface{})
		if !ok {
			continue
		}
		if m["url"] != extensionURLBlobRef {
			continue
		}
		raw, ok := m["valueString"].(string)
		if !ok {
			continue
		}
		var ref BlobReference
		if err := json.Unmarshal([]byte(raw), &ref); err != nil {
			return nil, err
		}
		return &ref, nil
	}
	return nil, ErrNotFound
}

// ResourceHasPayloadBytes reports whether FHIR JSON embeds inline binary payload bytes.
func ResourceHasPayloadBytes(data []byte) bool {
	obj, err := decodeJSONObject(data)
	if err != nil {
		return false
	}
	rt, _ := obj["resourceType"].(string)
	if rt == "Binary" {
		if dataVal, ok := obj["data"]; ok && dataVal != nil && dataVal != "" {
			return true
		}
	}
	return hasInlineAttachmentData(obj)
}

func hasInlineAttachmentData(v interface{}) bool {
	switch t := v.(type) {
	case map[string]interface{}:
		if att, ok := t["attachment"].(map[string]interface{}); ok {
			if dataVal, ok := att["data"]; ok && dataVal != nil && dataVal != "" {
				return true
			}
		}
		for _, child := range t {
			if hasInlineAttachmentData(child) {
				return true
			}
		}
	case []interface{}:
		for _, item := range t {
			if hasInlineAttachmentData(item) {
				return true
			}
		}
	}
	return false
}

func collectBlobRefs(v interface{}, refs *[]BlobReference) {
	switch t := v.(type) {
	case map[string]interface{}:
		if att, ok := t["attachment"].(map[string]interface{}); ok {
			if raw, ok := att[attachmentExtKey]; ok {
				if ref, ok := decodeBlobRef(raw); ok {
					*refs = append(*refs, ref)
				}
			}
		}
		for _, child := range t {
			collectBlobRefs(child, refs)
		}
	case []interface{}:
		for _, item := range t {
			collectBlobRefs(item, refs)
		}
	}
}

func decodeBlobRef(raw interface{}) (BlobReference, bool) {
	switch t := raw.(type) {
	case map[string]interface{}:
		b, err := json.Marshal(t)
		if err != nil {
			return BlobReference{}, false
		}
		var ref BlobReference
		if err := json.Unmarshal(b, &ref); err != nil {
			return BlobReference{}, false
		}
		return ref, true
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return BlobReference{}, false
		}
		var ref BlobReference
		if err := json.Unmarshal(b, &ref); err != nil {
			return BlobReference{}, false
		}
		return ref, true
	}
}

func decodeJSONObject(data []byte) (map[string]interface{}, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("%w: JSON must be an object", ErrInvalidArgument)
	}
	return obj, nil
}

func contentArray(obj map[string]interface{}) ([]interface{}, error) {
	raw, ok := obj["content"]
	if !ok {
		content := make([]interface{}, 0)
		obj["content"] = content
		return content, nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%w: content must be an array", ErrInvalidArgument)
	}
	return arr, nil
}

func mustMarshalRef(ref BlobReference) string {
	b, err := json.Marshal(ref)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// DescriptorToReference converts a BlobDescriptor to embeddable metadata.
func DescriptorToReference(desc BlobDescriptor) BlobReference {
	return BlobReference{
		BlobID:      desc.BlobID,
		SHA256:      desc.SHA256,
		Size:        desc.Size,
		ContentType: desc.ContentType,
		Pointer:     desc.Pointer,
	}
}
