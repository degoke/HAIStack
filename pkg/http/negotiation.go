package http

import (
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type notAcceptableError struct{ value string }

type responseFormat string

const (
	responseFormatJSON responseFormat = "json"
	responseFormatXML  responseFormat = "xml"
)

func (e *notAcceptableError) Error() string {
	return fmt.Sprintf("no supported FHIR representation is acceptable for %q", e.value)
}

func validateNegotiation(r *http.Request) error {
	_, err := negotiateResponseFormat(r)
	return err
}

func negotiateResponseFormat(r *http.Request) (responseFormat, error) {
	if r == nil {
		return responseFormatJSON, nil
	}
	if format := strings.TrimSpace(r.URL.Query().Get("_format")); format != "" {
		if parsed, ok := formatValue(format); ok {
			return parsed, nil
		}
		return responseFormatJSON, &notAcceptableError{value: format}
	}
	accept := strings.TrimSpace(r.Header.Get("Accept"))
	if accept == "" {
		return responseFormatJSON, nil
	}
	type candidate struct {
		format      responseFormat
		q           float64
		specificity int
		order       int
		matched     bool
	}
	best := map[responseFormat]candidate{}
	order := 0
	for _, item := range strings.Split(accept, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		mediaType, params, err := mime.ParseMediaType(item)
		if err != nil {
			return responseFormatJSON, &notAcceptableError{value: accept}
		}
		q := 1.0
		if qRaw := params["q"]; qRaw != "" {
			q, err = strconv.ParseFloat(qRaw, 64)
			if err != nil || q < 0 || q > 1 {
				return responseFormatJSON, &notAcceptableError{value: accept}
			}
		}
		for _, format := range []responseFormat{responseFormatJSON, responseFormatXML} {
			specificity := mediaRangeSpecificity(mediaType, format)
			if specificity < 0 {
				continue
			}
			current, exists := best[format]
			if !exists || specificity > current.specificity || (specificity == current.specificity && q > current.q) {
				best[format] = candidate{format: format, q: q, specificity: specificity, order: order, matched: true}
			}
		}
		order++
	}
	if len(best) == 0 {
		return responseFormatJSON, &notAcceptableError{value: accept}
	}
	candidates := make([]candidate, 0, len(best))
	for _, candidate := range best {
		if candidate.matched && candidate.q > 0 {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return responseFormatJSON, &notAcceptableError{value: accept}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].q == candidates[j].q {
			return candidates[i].order < candidates[j].order
		}
		return candidates[i].q > candidates[j].q
	})
	return candidates[0].format, nil
}

func mediaRangeSpecificity(mediaType string, format responseFormat) int {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if parsed, ok := formatValue(mediaType); ok {
		if parsed == format {
			return 3
		}
		return -1
	}
	switch mediaType {
	case "*/*":
		return 0
	case "application/*":
		return 1
	case "text/*":
		if format == responseFormatXML {
			return 1
		}
	}
	if strings.HasSuffix(mediaType, "/*+json") && format == responseFormatJSON {
		return 1
	}
	if strings.HasSuffix(mediaType, "/*+xml") && format == responseFormatXML {
		return 1
	}
	return -1
}

func isJSONFormat(value string) bool {
	format, ok := formatValue(value)
	return ok && format == responseFormatJSON
}

func formatValue(value string) (responseFormat, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "json", "application/json", "application/fhir+json":
		return responseFormatJSON, true
	case "xml", "application/xml", "text/xml", "application/fhir+xml":
		return responseFormatXML, true
	default:
		return "", false
	}
}
