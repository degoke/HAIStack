package http

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func marshalHistoryBundle(basePath, resourceType, id string, versions []store.ResourceVersion) ([]byte, error) {
	entries := make([]map[string]interface{}, 0, len(versions))
	for _, version := range versions {
		entry := map[string]interface{}{
			"fullUrl": historyLocation(basePath, resourceType, id, version.VersionID),
		}
		if version.Resource != nil && len(version.Resource.JSON) > 0 {
			var resourceObj interface{}
			if err := json.Unmarshal(version.Resource.JSON, &resourceObj); err != nil {
				return nil, fmt.Errorf("unmarshal history resource: %w", err)
			}
			entry["resource"] = resourceObj
		}
		method := historyMethod(version.Action)
		entry["request"] = map[string]interface{}{
			"method": method,
			"url":    historyRequestURL(resourceType, id, version.Action),
		}
		entries = append(entries, entry)
	}

	obj := map[string]interface{}{
		"resourceType": "Bundle",
		"type":         "history",
		"entry":        entries,
	}
	return json.Marshal(obj)
}

func historyMethod(action store.VersionAction) string {
	switch action {
	case store.VersionActionCreate:
		return "POST"
	case store.VersionActionUpdate:
		return "PUT"
	case store.VersionActionDelete:
		return "DELETE"
	default:
		return "GET"
	}
}

func historyRequestURL(resourceType, id string, action store.VersionAction) string {
	switch action {
	case store.VersionActionCreate:
		return resourceType
	default:
		return resourceType + "/" + id
	}
}

func marshalSearchBundle(bundle *search.SearchBundle) ([]byte, error) {
	if bundle == nil {
		bundle = &search.SearchBundle{}
	}
	obj := map[string]interface{}{
		"resourceType": "Bundle",
		"type":         "searchset",
	}
	if bundle.Total != nil {
		obj["total"] = *bundle.Total
	}
	if len(bundle.Links) > 0 {
		links := make([]map[string]string, 0, len(bundle.Links))
		for relation, url := range bundle.Links {
			links = append(links, map[string]string{
				"relation": relation,
				"url":      url,
			})
		}
		obj["link"] = links
	}
	entries := make([]map[string]interface{}, 0, len(bundle.Entries))
	for _, entry := range bundle.Entries {
		item := map[string]interface{}{
			"fullUrl": entry.FullURL,
		}
		if entry.Resource != nil && len(entry.Resource.JSON) > 0 {
			var resourceObj interface{}
			if err := json.Unmarshal(entry.Resource.JSON, &resourceObj); err != nil {
				return nil, fmt.Errorf("unmarshal search resource: %w", err)
			}
			item["resource"] = resourceObj
		}
		if entry.Mode != "" {
			item["search"] = map[string]string{"mode": entry.Mode}
		}
		entries = append(entries, item)
	}
	obj["entry"] = entries
	return json.Marshal(obj)
}

var platformCapabilityResourceTypes = []string{
	"CodeSystem",
	"ValueSet",
	"ConceptMap",
	"Basic",
	"CapabilityStatement",
	"Questionnaire",
	"QuestionnaireResponse",
	"ViewDefinition",
	"Library",
	"Group",
}

type capabilityFlags struct {
	Search             bool
	Terminology        bool
	BulkExport         bool
	SDC                bool
	Validate           bool
	ViewRun            bool
	ViewExport         bool
	Materialize        bool
	SQLQuery           bool
	PackageInstall     bool
	ModuleInstall      bool
	JobStatus          bool
	TerminologyInstall bool
	TerminologyEnable  bool
	ConformanceRefresh bool
}

func capabilityFromConfig(cfg Config) capabilityFlags {
	return capabilityFlags{
		Search:             cfg.SearchService != nil,
		Terminology:        cfg.TerminologyService != nil,
		BulkExport:         cfg.BulkExportService != nil,
		SDC:                cfg.SDCService != nil,
		Validate:           cfg.ValidateService != nil,
		ViewRun:            cfg.ViewRunService != nil,
		ViewExport:         cfg.ViewExportService != nil,
		Materialize:        cfg.ViewMaterializeService != nil,
		SQLQuery:           cfg.SQLQueryService != nil,
		PackageInstall:     cfg.PackageInstallService != nil,
		ModuleInstall:      cfg.ModuleInstallService != nil,
		JobStatus:          cfg.JobStatusService != nil,
		TerminologyInstall: cfg.TerminologyInstallService != nil,
		TerminologyEnable:  cfg.TerminologyEnableService != nil,
		ConformanceRefresh: cfg.ConformanceRefresher != nil,
	}
}

func augmentCapabilitySnapshot(snapshot registry.CapabilitySnapshot, flags capabilityFlags) (registry.CapabilitySnapshot, map[string]bool) {
	seen := make(map[string]bool, len(snapshot.Resources))
	injected := make(map[string]bool)
	for _, res := range snapshot.Resources {
		seen[res.ResourceType] = true
	}
	want := map[string]bool{}
	if flags.Terminology {
		want["CodeSystem"] = true
		want["ValueSet"] = true
		want["ConceptMap"] = true
	}
	if flags.ModuleInstall || flags.JobStatus || flags.TerminologyInstall || flags.TerminologyEnable {
		want["Basic"] = true
	}
	if flags.ConformanceRefresh {
		want["CapabilityStatement"] = true
	}
	if flags.SDC {
		want["Questionnaire"] = true
		want["QuestionnaireResponse"] = true
	}
	if flags.ViewRun || flags.ViewExport || flags.Materialize {
		want["ViewDefinition"] = true
	}
	if flags.SQLQuery {
		want["Library"] = true
	}
	if flags.BulkExport {
		want["Group"] = true
		want["Patient"] = true
	}
	for _, typ := range platformCapabilityResourceTypes {
		if !want[typ] || seen[typ] {
			continue
		}
		snapshot.Resources = append(snapshot.Resources, registry.ResourceCapability{ResourceType: typ})
		seen[typ] = true
		injected[typ] = true
	}
	return snapshot, injected
}

func marshalCapabilityStatement(snapshot registry.CapabilitySnapshot, meta ServerMetadata, flags capabilityFlags) ([]byte, error) {
	snapshot, platformOnly := augmentCapabilitySnapshot(snapshot, flags)
	rest := make([]map[string]interface{}, 0, 1)
	resourceEntries := make([]map[string]interface{}, 0, len(snapshot.Resources))
	for _, res := range snapshot.Resources {
		var interactions []map[string]string
		if !platformOnly[res.ResourceType] {
			interactions = []map[string]string{
				{"code": "read"},
				{"code": "vread"},
				{"code": "create"},
				{"code": "update"},
				{"code": "patch"},
				{"code": "delete"},
				{"code": "history-instance"},
			}
			if flags.Search {
				interactions = append(interactions, map[string]string{"code": "search-type"})
			}
		}
		operations := resourceOperations(res.ResourceType, flags)
		entry := map[string]interface{}{
			"type":         res.ResourceType,
			"interaction":  interactions,
			"updateCreate": false,
		}
		if len(operations) > 0 {
			entry["operation"] = operations
		}
		if platformOnly[res.ResourceType] {
			entry["readHistory"] = false
		} else {
			entry["searchParam"] = searchParamsForCapability(res.SearchParameters)
			entry["versioning"] = "versioned"
			entry["readHistory"] = true
		}
		resourceEntries = append(resourceEntries, entry)
	}
	restEntry := map[string]interface{}{
		"mode":     "server",
		"resource": resourceEntries,
	}
	if systemOps := systemOperations(flags); len(systemOps) > 0 {
		restEntry["operation"] = systemOps
	}
	rest = append(rest, restEntry)

	software := map[string]interface{}{}
	if meta.SoftwareName != "" {
		software["name"] = meta.SoftwareName
	}
	if meta.SoftwareVersion != "" {
		software["version"] = meta.SoftwareVersion
	}

	implementation := map[string]interface{}{}
	if meta.ServerName != "" {
		implementation["description"] = meta.ServerName
	}

	obj := map[string]interface{}{
		"resourceType": "CapabilityStatement",
		"status":       "active",
		"date":         snapshot.CompiledAt.UTC().Format(time.RFC3339),
		"kind":         "instance",
		"fhirVersion":  snapshot.FHIRVersion,
		"format":       []string{"application/fhir+json", "application/fhir+xml"},
		"patchFormat":  []string{"application/json-patch+json", "application/fhir+json"},
		"rest":         rest,
	}
	if len(software) > 0 {
		obj["software"] = software
	}
	if len(implementation) > 0 {
		obj["implementation"] = implementation
	}
	if meta.Description != "" {
		obj["description"] = meta.Description
	}
	return json.Marshal(obj)
}

func resourceOperations(resourceType string, flags capabilityFlags) []map[string]string {
	var operations []map[string]string
	if flags.Validate && resourceType != "" && !platformOnlyValidateSkip(resourceType) {
		operations = append(operations, map[string]string{
			"name":       "validate",
			"definition": "http://hl7.org/fhir/OperationDefinition/Resource-validate",
		})
	}
	switch resourceType {
	case "Patient":
		operations = append(operations, map[string]string{
			"name":       "everything",
			"definition": "http://hl7.org/fhir/OperationDefinition/Patient-everything",
		})
		if flags.BulkExport {
			operations = append(operations, map[string]string{
				"name":       "export",
				"definition": "http://hl7.org/fhir/uv/bulkdata/OperationDefinition/export",
			})
		}
	case "Group":
		if flags.BulkExport {
			operations = append(operations, map[string]string{
				"name":       "export",
				"definition": "http://hl7.org/fhir/uv/bulkdata/OperationDefinition/group-export",
			})
		}
	case "ImplementationGuide":
		if flags.PackageInstall {
			operations = append(operations, map[string]string{
				"name":       "install",
				"definition": "http://hl7.org/fhir/OperationDefinition/ImplementationGuide-install",
			})
		}
	case "CodeSystem":
		if flags.Terminology {
			operations = append(operations,
				map[string]string{
					"name":       "lookup",
					"definition": "http://hl7.org/fhir/OperationDefinition/CodeSystem-lookup",
				},
				map[string]string{
					"name":       "validate-code",
					"definition": "http://hl7.org/fhir/OperationDefinition/CodeSystem-validate-code",
				},
			)
		}
	case "ValueSet":
		if flags.Terminology {
			operations = append(operations,
				map[string]string{
					"name":       "expand",
					"definition": "http://hl7.org/fhir/OperationDefinition/ValueSet-expand",
				},
				map[string]string{
					"name":       "validate-code",
					"definition": "http://hl7.org/fhir/OperationDefinition/ValueSet-validate-code",
				},
			)
		}
	case "ConceptMap":
		if flags.Terminology {
			operations = append(operations, map[string]string{
				"name":       "translate",
				"definition": "http://hl7.org/fhir/OperationDefinition/ConceptMap-translate",
			})
		}
	case "Basic":
		if flags.ModuleInstall {
			operations = append(operations, map[string]string{
				"name":       "install",
				"definition": "http://hl7.org/fhir/OperationDefinition/Basic-install",
			})
		}
		if flags.JobStatus {
			operations = append(operations, map[string]string{
				"name":       "status",
				"definition": "http://hl7.org/fhir/OperationDefinition/Basic-status",
			})
		}
		if flags.TerminologyInstall {
			operations = append(operations, map[string]string{
				"name":       "terminology-install",
				"definition": "http://hl7.org/fhir/OperationDefinition/Basic-terminology-install",
			})
		}
		if flags.TerminologyEnable {
			operations = append(operations, map[string]string{
				"name":       "terminology-enable",
				"definition": "http://hl7.org/fhir/OperationDefinition/Basic-terminology-enable",
			})
		}
	case "CapabilityStatement":
		if flags.ConformanceRefresh {
			operations = append(operations, map[string]string{
				"name":       "refresh",
				"definition": "http://hl7.org/fhir/OperationDefinition/CapabilityStatement-refresh",
			})
		}
	case "Questionnaire":
		if flags.SDC {
			operations = append(operations,
				map[string]string{"name": "populate", "definition": "http://hl7.org/fhir/uv/sdc/OperationDefinition/Questionnaire-populate"},
				map[string]string{"name": "assemble", "definition": "http://hl7.org/fhir/uv/sdc/OperationDefinition/Questionnaire-assemble"},
			)
		}
	case "QuestionnaireResponse":
		if flags.SDC {
			operations = append(operations,
				map[string]string{"name": "extract", "definition": "http://hl7.org/fhir/uv/sdc/OperationDefinition/QuestionnaireResponse-extract"},
			)
		}
	case "ViewDefinition":
		if flags.ViewRun {
			operations = append(operations, map[string]string{
				"name": "viewdefinition-run", "definition": "http://hl7.org/fhir/uv/sql-on-fhir/OperationDefinition/ViewDefinition-run",
			})
		}
		if flags.ViewExport {
			operations = append(operations, map[string]string{
				"name": "viewdefinition-export", "definition": "http://hl7.org/fhir/uv/sql-on-fhir/OperationDefinition/ViewDefinition-export",
			})
		}
		if flags.Materialize {
			operations = append(operations, map[string]string{
				"name": "materialize", "definition": "http://hl7.org/fhir/uv/sql-on-fhir/OperationDefinition/ViewDefinition-materialize",
			})
		}
	case "Library":
		if flags.SQLQuery {
			operations = append(operations, map[string]string{
				"name": "sqlquery-run", "definition": "http://hl7.org/fhir/uv/sql-on-fhir/OperationDefinition/sqlquery-run",
			})
		}
	}
	return operations
}

func platformOnlyValidateSkip(resourceType string) bool {
	switch resourceType {
	case "Basic", "CapabilityStatement", "ViewDefinition", "Library":
		return true
	default:
		return false
	}
}

func systemOperations(flags capabilityFlags) []map[string]string {
	var ops []map[string]string
	if flags.BulkExport {
		ops = append(ops, map[string]string{
			"name":       "export",
			"definition": "http://hl7.org/fhir/uv/bulkdata/OperationDefinition/export",
		})
	}
	if flags.ViewRun {
		ops = append(ops, map[string]string{
			"name": "viewdefinition-run", "definition": "http://hl7.org/fhir/uv/sql-on-fhir/OperationDefinition/ViewDefinition-run",
		})
	}
	if flags.SQLQuery {
		ops = append(ops, map[string]string{
			"name": "sqlquery-run", "definition": "http://hl7.org/fhir/uv/sql-on-fhir/OperationDefinition/sqlquery-run",
		})
	}
	return ops
}

func searchParamsForCapability(params []registry.SearchParameterInfo) []map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(params))
	for _, param := range params {
		out = append(out, map[string]string{
			"name": param.Code,
			"type": param.Type,
		})
	}
	return out
}

func isTransactionBundle(data []byte) (bool, error) {
	bundleType, err := bundleTypeFromBody(data)
	if err != nil {
		return false, err
	}
	return bundleType == "transaction", nil
}

func isBatchBundle(data []byte) (bool, error) {
	bundleType, err := bundleTypeFromBody(data)
	if err != nil {
		return false, err
	}
	return bundleType == "batch", nil
}

func bundleTypeFromBody(data []byte) (string, error) {
	normalized, err := types.NormalizeJSON(data)
	if err != nil {
		return "", err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(normalized, &obj); err != nil {
		return "", err
	}
	resourceType, _ := obj["resourceType"].(string)
	if resourceType != "Bundle" {
		return "", nil
	}
	bundleType, _ := obj["type"].(string)
	return bundleType, nil
}
