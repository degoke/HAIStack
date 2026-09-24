package ai

import "testing"

func TestAnalyzeSearchParamGrounding_NoSearchTool(t *testing.T) {
	warnings := AnalyzeSearchParamGrounding("We used birthdate search for the patient.", nil)
	if len(warnings) == 0 {
		t.Fatal("expected warning when narrating search without tool")
	}
}

func TestAnalyzeSearchParamGrounding_UnknownParam(t *testing.T) {
	records := toolCallRecords([]HarnessToolResult{{
		ToolName: ToolSearchFhirResources,
	}}, []map[string]any{{
		"resourceType": "Patient",
		"params":       map[string]any{"name": "Jane"},
	}})
	warnings := AnalyzeSearchParamGrounding("We used birthdate search for the patient.", records)
	if len(warnings) == 0 {
		t.Fatal("expected search param warning")
	}
}
