package ai_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/degoke/haistack/pkg/ai"
)

func TestAnalyzeAnswerGrounding_UncitedRef(t *testing.T) {
	warnings := ai.AnalyzeAnswerGrounding("The patient Patient/unknown-1 is stable.", []ai.Citation{
		{Kind: "resource", Ref: "Patient/pat-jane"},
	})
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestHarness_GroundingStrictRequiresToolEvidence(t *testing.T) {
	h := newTestHarness(t, harnessOptions{})
	model := &recordingChatModel{responses: []*ai.ChatResponse{{Content: "Jane is fine."}}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:  h.exec,
		Model:     model,
		Actor:     "agent",
		Grounding: ai.GroundingConfig{Mode: ai.GroundingStrict},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.Chat(context.Background(), "Who is the patient in the chart?")
	if !errors.Is(err, ai.ErrUngroundedAnswer) {
		t.Fatalf("err = %v", err)
	}
}

func TestHarness_GroundingStandardWarnsButSucceeds(t *testing.T) {
	h := newTestHarness(t, harnessOptions{})
	model := &recordingChatModel{responses: []*ai.ChatResponse{{Content: "Patient/unknown-9 is listed."}}}
	harness, err := ai.NewHarness(ai.HarnessConfig{
		Executor:  h.exec,
		Model:     model,
		Actor:     "agent",
		Grounding: ai.GroundingConfig{Mode: ai.GroundingStandard},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := harness.Chat(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.GroundingWarnings) == 0 {
		t.Fatal("expected warnings in standard mode")
	}
}

func TestAppendGroundingSystemPrompt(t *testing.T) {
	out := ai.AppendGroundingSystemPrompt("Host rules.", ai.GroundingConfig{Mode: ai.GroundingStandard})
	if !strings.Contains(out, "FHIR server") || !strings.Contains(out, "Host rules") {
		t.Fatalf("prompt = %q", out)
	}
}
