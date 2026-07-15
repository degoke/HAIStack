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
	routeType
	routeInstance
	routeHistory
)

type parsedRoute struct {
	kind         routeKind
	resourceType string
	id           string
}

func parseRoute(basePath, requestPath string) (parsedRoute, error) {
	rel, err := relativePath(basePath, requestPath)
	if err != nil {
		return parsedRoute{}, err
	}
	if rel == "" {
		return parsedRoute{kind: routeTransaction}, nil
	}

	parts := strings.Split(rel, "/")
	switch len(parts) {
	case 1:
		if parts[0] == "metadata" {
			return parsedRoute{kind: routeMetadata}, nil
		}
		if !validResourceType(parts[0]) {
			return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
		}
		return parsedRoute{kind: routeType, resourceType: parts[0]}, nil
	case 2:
		if !validResourceType(parts[0]) {
			return parsedRoute{}, fmt.Errorf("invalid resource type %q", parts[0])
		}
		if err := validateID(parts[1]); err != nil {
			return parsedRoute{}, err
		}
		return parsedRoute{kind: routeInstance, resourceType: parts[0], id: parts[1]}, nil
	case 3:
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
	for _, r := range resourceType {
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
	return &core.ServiceError{
		Kind:    core.ErrorKindNotSupported,
		Message: fmt.Sprintf("method %s is not supported for this endpoint", method),
	}
}

func unsupportedEndpoint(path string) error {
	return &core.ServiceError{
		Kind:    core.ErrorKindNotSupported,
		Message: fmt.Sprintf("endpoint %q is not supported", path),
	}
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

func writeMethodNotAllowed(w http.ResponseWriter, method string) {
	writeError(w, methodNotAllowed(method))
}
