package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// DefaultMaxToolRounds bounds the model ↔ tool loop in Harness.Chat.
const DefaultMaxToolRounds = 8

// HarnessConfig wires an upstream app to Executor-backed tools through a ChatModel.
type HarnessConfig struct {
	Executor     *Executor
	Model        ChatModel
	SystemPrompt string
	Actor        string
	TenantID     string
	Subject      string
	// MaxToolRounds limits model completion rounds that include tool calls (default 8).
	MaxToolRounds int
	// ToolContextFormat selects JSON (default) or markdown summaries for tool messages.
	ToolContextFormat ToolContextFormat
	// EnablePatientCreateHelper exposes propose_patient_create in the harness tool list (Phase C).
	EnablePatientCreateHelper bool
}

// Harness orchestrates conversation turns: model completions, tool execution via
// Executor, and in-memory session history. It does not replace policy, audit, or
// FHIR validation on the executor path.
type Harness struct {
	cfg     HarnessConfig
	session Session
}

// Session holds conversation identity and message history (in-memory v1).
type Session struct {
	ConversationID string
	Messages       []ChatMessage
}

// ChatResult is the outcome of one Harness.Chat user turn.
type ChatResult struct {
	Answer      string
	Messages    []ChatMessage
	ToolResults []HarnessToolResult
}

// HarnessToolResult summarizes one tool invocation performed during Chat.
type HarnessToolResult struct {
	ToolName string
	Result   *ToolResult
	Err      error
}

// NewHarness validates configuration and returns a harness with an empty session.
func NewHarness(cfg HarnessConfig) (*Harness, error) {
	if cfg.Executor == nil {
		return nil, errors.New("ai: harness requires Executor")
	}
	if cfg.Model == nil {
		return nil, errors.New("ai: harness requires ChatModel")
	}
	if cfg.MaxToolRounds <= 0 {
		cfg.MaxToolRounds = DefaultMaxToolRounds
	}
	return &Harness{cfg: cfg}, nil
}

// NewHarnessWithSession returns a harness that continues an existing session.
func NewHarnessWithSession(cfg HarnessConfig, session Session) (*Harness, error) {
	h, err := NewHarness(cfg)
	if err != nil {
		return nil, err
	}
	h.session = session
	return h, nil
}

// Session returns the current in-memory session (including messages appended by Chat).
func (h *Harness) Session() Session {
	if h == nil {
		return Session{}
	}
	return h.session
}

// SetConversationID sets the correlation id used on executor tool requests.
func (h *Harness) SetConversationID(id string) {
	if h == nil {
		return
	}
	h.session.ConversationID = id
}

// Chat handles one user message: model completion, optional tool calls through
// Executor, and a final natural-language answer when the model stops requesting tools.
func (h *Harness) Chat(ctx context.Context, userMessage string) (*ChatResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	userMessage = trimSpace(userMessage)
	if userMessage == "" {
		return nil, ErrInvalidInput
	}
	h.session.Messages = append(h.session.Messages, ChatMessage{
		Role:    ChatRoleUser,
		Content: userMessage,
	})

	tools := h.chatTools()
	toolSummaries := make([]HarnessToolResult, 0)

	for round := 0; round < h.cfg.MaxToolRounds; round++ {
		resp, err := h.cfg.Model.Chat(ctx, ChatRequest{
			Messages:     h.session.Messages,
			Tools:        tools,
			SystemPrompt: h.cfg.SystemPrompt,
		})
		if err != nil {
			return nil, err
		}
		assistant := ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		h.session.Messages = append(h.session.Messages, assistant)

		if len(resp.ToolCalls) == 0 {
			return &ChatResult{
				Answer:      resp.Content,
				Messages:    append([]ChatMessage(nil), h.session.Messages...),
				ToolResults: toolSummaries,
			}, nil
		}

		for _, tc := range resp.ToolCalls {
			summary := HarnessToolResult{ToolName: tc.Name}
			input, parseErr := ParseToolArguments(tc.Arguments)
			if parseErr != nil {
				summary.Err = fmt.Errorf("ai: tool %q arguments: %w", tc.Name, parseErr)
				toolSummaries = append(toolSummaries, summary)
				h.session.Messages = append(h.session.Messages, ChatMessage{
					Role:       ChatRoleTool,
					ToolCallID: tc.ID,
					Content:    toolErrorContent(summary.Err),
				})
				continue
			}
			if tc.Name == ToolProposePatientCreate {
				content, proposeRes, proposeErr := h.handleProposePatientCreate(input)
				summary.Err = proposeErr
				if proposeRes != nil {
					summary.Result = proposeRes
				}
				toolSummaries = append(toolSummaries, summary)
				h.session.Messages = append(h.session.Messages, ChatMessage{
					Role:       ChatRoleTool,
					ToolCallID: tc.ID,
					Content:    content,
				})
				continue
			}
			res, execErr := h.cfg.Executor.ExecuteTool(ctx, ToolRequest{
				ToolName:       tc.Name,
				Actor:          h.cfg.Actor,
				TenantID:       h.cfg.TenantID,
				Subject:        h.cfg.Subject,
				Input:          input,
				ConversationID: h.session.ConversationID,
			})
			summary.Result = res
			summary.Err = execErr
			toolSummaries = append(toolSummaries, summary)

			content := h.formatToolResultContent(tc.Name, res, execErr)
			h.session.Messages = append(h.session.Messages, ChatMessage{
				Role:       ChatRoleTool,
				ToolCallID: tc.ID,
				Content:    content,
			})
		}
	}

	return nil, fmt.Errorf("ai: exceeded max tool rounds (%d)", h.cfg.MaxToolRounds)
}

func (h *Harness) chatTools() []ChatTool {
	descriptors := h.cfg.Executor.ToolDescriptors()
	if h.cfg.EnablePatientCreateHelper {
		descriptors = append(descriptors, ProposePatientCreateToolDescriptor())
	}
	return ChatToolsFromDescriptors(descriptors)
}

func (h *Harness) handleProposePatientCreate(input map[string]any) (string, *ToolResult, error) {
	draft, err := PatientCreateDraftFromMap(input)
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	writeInput, err := draft.ToWriteFhirResourceInput()
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	payload := map[string]any{
		"status":                 "proposal",
		"write_fhir_resource":    writeInput,
		"commitHint":             "Call Harness.CommitPatientCreate with the same draft to execute policy + validation.",
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	return string(out), &ToolResult{
		ToolName: ToolProposePatientCreate,
		Data:     payload,
		Context:  string(out),
	}, nil
}

func (h *Harness) formatToolResultContent(toolName string, res *ToolResult, err error) string {
	if err != nil {
		return toolErrorContent(err)
	}
	if res == nil {
		return "{}"
	}
	if h.cfg.ToolContextFormat == ToolContextMarkdown {
		md, mdErr := NewMarkdownContextBuilder().FormatToolResult(toolName, res.Data, res.Citations)
		if mdErr == nil && md != "" {
			return md
		}
	}
	if res.Context != "" {
		return res.Context
	}
	out, marshalErr := json.Marshal(res.Data)
	if marshalErr != nil {
		return fmt.Sprintf("{\"error\":\"%s\"}", marshalErr.Error())
	}
	return string(out)
}

func toolErrorContent(err error) string {
	if err == nil {
		return "{}"
	}
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}
