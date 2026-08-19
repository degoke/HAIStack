package http

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
)

var fhirIDPattern = regexp.MustCompile(`^[A-Za-z0-9\-\.]{1,64}$`)

type routeKind int

const (
	routeUnknown routeKind = iota
	routeMetadata
	routeTransaction
	routeSystemSearch
	routeType
	routeTypeSearch
	routeInstance
	routeHistory
	routeOperation
	routeSystemOperation
)

type parsedRoute struct {
	kind         routeKind
	resourceType string
	id           string
	operation    string
}

func parseRoute(basePath, requestPath string) (parsedRoute, error) {
	rel, err := relativePath(basePath, requestPath)
	if err != nil {
		return parsedRoute{}, err
	}
	if rel == "" {
		return parsedRoute{kind: routeTransaction}, nil
	}
	if rel == "_search" {
		return parsedRoute{kind: routeSystemSearch}, nil
	}

	parts := strings.Split(rel, "/")
	switch len(parts) {
	case 1:
		if parts[0] == "metadata" {
			return parsedRoute{kind: routeMetadata}, nil
		}
		if strings.HasPrefix(parts[0], "$") {
			if !validOperation(parts[0]) {
				return parsedRoute{}, fmt.Errorf("invalid operation %q", parts[0])
			}
			return parsedRoute{kind: routeSystemOperation, operation: parts[0]}, nil
		}
		if !validResourceType(parts[0]) {
			return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
		}
		return parsedRoute{kind: routeType, resourceType: parts[0]}, nil
	case 2:
		if parts[1] == "_search" {
			if !validResourceType(parts[0]) {
				return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
			}
			return parsedRoute{kind: routeTypeSearch, resourceType: parts[0]}, nil
		}
		if strings.HasPrefix(parts[1], "$") {
			if !validOperation(parts[1]) {
				return parsedRoute{}, fmt.Errorf("invalid operation %q", parts[1])
			}
			if !validResourceType(parts[0]) {
				return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
			}
			return parsedRoute{kind: routeOperation, resourceType: parts[0], operation: parts[1]}, nil
		}
		if !validResourceType(parts[0]) {
			return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
		}
		if err := validateID(parts[1]); err != nil {
			return parsedRoute{}, err
		}
		return parsedRoute{kind: routeInstance, resourceType: parts[0], id: parts[1]}, nil
	case 3:
		if strings.HasPrefix(parts[2], "$") {
			if !validOperation(parts[2]) {
				return parsedRoute{}, fmt.Errorf("invalid operation %q", parts[2])
			}
			if !validResourceType(parts[0]) {
				return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
			}
			if err := validateID(parts[1]); err != nil {
				return parsedRoute{}, err
			}
			return parsedRoute{kind: routeOperation, resourceType: parts[0], id: parts[1], operation: parts[2]}, nil
		}
		if parts[2] != "_history" {
			return parsedRoute{}, fmt.Errorf("unsupported path segment %q", parts[2])
		}
		if !validResourceType(parts[0]) {
			return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
		}
		if err := validateID(parts[1]); err != nil {
			return parsedRoute{}, err
		}
		return parsedRoute{kind: routeHistory, resourceType: parts[0], id: parts[1]}, nil
	default:
		return parsedRoute{}, fmt.Errorf("unsupported path %q", rel)
	}
}
func validOperation(op string) bool {
	if len(op) < 2 || op[0] != '$' {
		return false
	}
	for _, r := range op[1:] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func relativePath(basePath, requestPath string) (string, error) {
	base := strings.TrimSuffix(basePath, "/")
	if base == "" {
		base = "/"
	}
	path := requestPath
	if !strings.HasPrefix(path, base) {
		return "", fmt.Errorf("path does not match base %q", basePath)
	}
	if base != "/" && len(path) > len(base) && path[len(base)] != '/' {
		return "", fmt.Errorf("path does not match base %q", basePath)
	}
	rel := strings.TrimPrefix(path, base)
	return strings.Trim(rel, "/"), nil
}

func validResourceType(resourceType string) bool {
	if resourceType == "" {
		return false
	}
	for i, r := range resourceType {
		if i == 0 && (r < 'A' || r > 'Z') {
			return false
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if !fhirIDPattern.MatchString(id) {
		return fmt.Errorf("id %q does not match FHIR id syntax", id)
	}
	return nil
}

func methodNotAllowed(method string) error {
	return &methodNotAllowedError{method: method}
}

type methodNotAllowedError struct{ method string }

func (e *methodNotAllowedError) Error() string {
	return fmt.Sprintf("method %s is not supported for this endpoint", e.method)
}

func unsupportedEndpoint(path string) error {
	return &core.ServiceError{
		Kind:    core.ErrorKindNotSupported,
		Message: fmt.Sprintf("endpoint %q is not supported", path),
	}
}

func notImplementedEndpoint(path string) error {
	return &notImplementedError{path: path}
}

type notImplementedError struct {
	path string
}

func (e *notImplementedError) Error() string {
	return fmt.Sprintf("endpoint %q is not implemented", e.path)
}

func invalidRequest(message string, cause error, expression ...string) error {
	return &core.ServiceError{
		Kind:       core.ErrorKindInvalid,
		Message:    message,
		Cause:      cause,
		Expression: expression,
	}
}

func idMismatch(pathID, bodyID string) error {
	return invalidRequest(
		fmt.Sprintf("id mismatch: path %q does not match body %q", pathID, bodyID),
		nil,
		"Resource.id",
	)
}

func isPathError(err error) bool {
	if err == nil {
		return false
	}
	var svcErr *core.ServiceError
	return !errors.As(err, &svcErr)
}

func locationURL(basePath, resourceType, id string) string {
	return strings.TrimSuffix(basePath, "/") + "/" + resourceType + "/" + id
}

func historyLocation(basePath, resourceType, id, versionID string) string {
	return strings.TrimSuffix(basePath, "/") + "/" + resourceType + "/" + id + "/_history/" + versionID
}

func contentTypeFHIRJSON() string {
	return "application/fhir+json"
}

func contentTypeFHIRXML() string {
	return "application/fhir+xml"
}

func writeMethodNotAllowed(w http.ResponseWriter, method string, allowed ...string) {
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
	}
	writeError(w, methodNotAllowed(method))
}
