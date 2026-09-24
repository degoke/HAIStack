package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/store"
	"github.com/google/uuid"
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
	// ModelHint is passed to ChatModel backends that support routing (e.g. local vs cloud).
	ModelHint string
	// MaxToolRounds limits model completion rounds that include tool calls (default 8).
	MaxToolRounds int
	// ToolContextFormat selects JSON (default) or markdown summaries for tool messages.
	ToolContextFormat ToolContextFormat
	// EnableProposeWriteHelper exposes propose_write_resource in the harness tool list.
	EnableProposeWriteHelper bool
	// EnablePatientCreateHelper is deprecated: use EnableProposeWriteHelper (kept for compatibility).
	EnablePatientCreateHelper bool
	// BlockDirectWriteTools removes create/update FHIR write tools from the model tool list so commits
	// go through structured helpers (e.g. CommitWrite) instead of free-form tool args.
	BlockDirectWriteTools bool
	// AutoConversationID assigns a UUID when Chat runs without SetConversationID (useful with RequireConversationID).
	AutoConversationID bool
	// ToolCallProtocol selects native function tools, prompt JSON in text, or both (default native).
	ToolCallProtocol ToolCallProtocol
	// SessionService persists ADK-style sessions (events + state).
	SessionService store.SessionService
	// AppName scopes sessions (ADK app_name); defaults to DefaultHarnessAppName.
	AppName string
	// SessionCompaction optionally summarizes older events into append-only checkpoint events.
	SessionCompaction SessionCompactionConfig
	// Grounding reduces hallucination via prompts, citation checks, and optional strict enforcement.
	Grounding GroundingConfig
	// RequireCommitConfirmation requires CommitWriteConfirm before CommitWrite / CommitWriteFromSession.
	RequireCommitConfirmation bool
	// CommitWriteConfirm is invoked before CommitWrite when the plan has a single entry (unless CommitWritePlanConfirm is set).
	CommitWriteConfirm func(ctx context.Context, draft ResourceWriteDraft) error
	// CommitWritePlanConfirm is invoked before CommitWritePlan (multi-step or when set explicitly).
	CommitWritePlanConfirm func(ctx context.Context, plan ResourceWritePlan) error
	// EnableProposeWritePlanHelper exposes propose_write_plan in the harness tool list.
	EnableProposeWritePlanHelper bool
}

// Harness orchestrates conversation turns: model completions, tool execution via
// Executor, and session history (persisted via SessionService when configured).
// It does not replace policy, audit, or FHIR validation on the executor path.
type Harness struct {
	cfg                HarnessConfig
	session            Session
	persistedEvents    []store.SessionEvent
	activeInvocationID string
	compactionMetrics  *compactionMetrics
}

// Session is the harness view of a stored agent session after load (or ephemeral test mode).
// ConversationID is set before Chat; Messages and State come from SessionService when configured.
type Session struct {
	ConversationID string
	Messages       []ChatMessage
	State          map[string]any
}

// ChatOptions configures one user turn. InvocationID enables idempotent retries when SessionService is set.
type ChatOptions struct {
	UserMessage  string
	InvocationID string
}

// ChatResult is the outcome of one Harness.Chat user turn.
type ChatResult struct {
	InvocationID     string
	Answer           string
	Messages         []ChatMessage
	ToolResults      []HarnessToolResult
	Citations          []Citation
	PendingApprovals   []HarnessPendingApproval
	GroundingWarnings  []string
}

// HarnessPendingApproval captures approval-required writes surfaced during Chat.
type HarnessPendingApproval struct {
	ToolName string
	Token    string
	Preview  any
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
	if cfg.SessionService != nil && strings.TrimSpace(cfg.TenantID) == "" {
		return nil, errors.New("ai: harness SessionService requires TenantID")
	}
	return &Harness{cfg: cfg}, nil
}

// NewHarnessWithSession binds a conversation id before Chat. With SessionService, transcript
// and state are always loaded from the store (preloaded Messages/State are ignored).
func NewHarnessWithSession(cfg HarnessConfig, session Session) (*Harness, error) {
	h, err := NewHarness(cfg)
	if err != nil {
		return nil, err
	}
	h.session = session
	return h, nil
}

// Session returns the loaded session view (from SessionService after load/Chat, or ephemeral test history).
func (h *Harness) Session() Session {
	if h == nil {
		return Session{}
	}
	return h.session
}

// PersistedEvents returns the full append-only event log (including events before compaction checkpoints).
func (h *Harness) PersistedEvents() []store.SessionEvent {
	if h == nil {
		return nil
	}
	return append([]store.SessionEvent(nil), h.persistedEvents...)
}

// ExecuteHarnessTool runs a single executor tool with harness actor/subject/conversation defaults.
// Use this to resume approval-gated writes with ApprovalToken after Chat surfaces PendingApprovals.
func (h *Harness) ExecuteHarnessTool(ctx context.Context, req ToolRequest) (*ToolResult, error) {
	return h.ExecuteHarnessToolWithOptions(ctx, req, CommitWriteOptions{})
}

// ExecuteHarnessToolWithOptions runs a harness tool with optional commit confirmation for writes.
func (h *Harness) ExecuteHarnessToolWithOptions(ctx context.Context, req ToolRequest, commitOpts CommitWriteOptions) (*ToolResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	if err := h.ensureSessionLoaded(ctx); err != nil {
		return nil, err
	}
	if req.Actor == "" {
		req.Actor = h.cfg.Actor
	}
	if req.TenantID == "" {
		req.TenantID = h.cfg.TenantID
	}
	if req.Subject == "" {
		req.Subject = h.cfg.Subject
	}
	if req.ConversationID == "" {
		req.ConversationID = h.session.ConversationID
	}
	if IsWriteTool(req.ToolName) {
		if err := h.confirmBeforeWriteToolInput(ctx, req.ToolName, req.Input, commitOpts); err != nil {
			return nil, err
		}
		return h.executeHarnessWriteTool(ctx, req.ToolName, req.Input)
	}
	return h.cfg.Executor.ExecuteTool(ctx, req)
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
	return h.ChatWithOptions(ctx, ChatOptions{UserMessage: userMessage})
}

// ChatWithOptions runs one user turn with an optional invocation id for idempotent retries.
func (h *Harness) ChatWithOptions(ctx context.Context, opts ChatOptions) (*ChatResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	userMessage := trimSpace(opts.UserMessage)
	if userMessage == "" {
		return nil, ErrInvalidInput
	}
	invocationID := trimSpace(opts.InvocationID)
	if invocationID == "" {
		invocationID = uuid.NewString()
	}
	if err := h.ensureSessionLoaded(ctx); err != nil {
		return nil, err
	}
	h.activeInvocationID = invocationID

	if answer, done := invocationTerminalAnswer(h.persistedEvents, invocationID); done {
		summaries, inputs := invocationToolGroundingContext(h.persistedEvents, invocationID, h.cfg.ToolContextFormat)
		return h.finalizeChatResult(invocationID, userMessage, answer, summaries, inputs)
	}

	tools := h.chatTools()
	_ = h.maybeCompactSession(ctx, tools)

	skipUserAppend := hasUserEventForInvocation(h.persistedEvents, invocationID)
	if !skipUserAppend {
		h.session.Messages = append(h.session.Messages, ChatMessage{
			Role:    ChatRoleUser,
			Content: userMessage,
		})
		if err := h.appendSessionEvent(ctx, NewUserSessionEvent(userMessage)); err != nil {
			h.activeInvocationID = ""
			return nil, err
		}
	}

	toolSummaries := make([]HarnessToolResult, 0)
	toolInputs := make([]map[string]any, 0)

	for round := 0; round < h.cfg.MaxToolRounds; round++ {
		resp, err := h.cfg.Model.Chat(ctx, ChatRequest{
			Messages:     h.session.Messages,
			Tools:        h.nativeToolsForModel(tools),
			SystemPrompt: h.effectiveSystemPrompt(tools),
			Hint:         h.cfg.ModelHint,
		})
		if err != nil {
			h.activeInvocationID = ""
			return nil, err
		}
		toolCalls, promptErr := h.resolveToolCalls(resp, tools)
		if promptErr != nil {
			h.activeInvocationID = ""
			return nil, promptErr
		}
		assistant := ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   resp.Content,
			ToolCalls: toolCalls,
		}
		h.session.Messages = append(h.session.Messages, assistant)
		if err := h.appendSessionEvent(ctx, NewModelSessionEvent(resp.Content, toolCalls)); err != nil {
			h.activeInvocationID = ""
			return nil, err
		}

		if len(toolCalls) == 0 {
			h.activeInvocationID = ""
			_ = h.maybeCompactSession(ctx, tools)
			return h.finalizeChatResult(invocationID, userMessage, resp.Content, toolSummaries, toolInputs)
		}

		for _, tc := range toolCalls {
			summary := HarnessToolResult{ToolName: tc.Name}
			input, parseErr := ParseToolArguments(tc.Arguments)
			if parseErr != nil {
				summary.Err = fmt.Errorf("ai: tool %q arguments: %w", tc.Name, parseErr)
				toolSummaries = append(toolSummaries, summary)
				toolInputs = append(toolInputs, input)
				errContent := toolErrorContent(summary.Err)
				// Tool-role message is sent to the model on the next completion round (same as executor errors).
				h.session.Messages = append(h.session.Messages, ChatMessage{
					Role:       ChatRoleTool,
					ToolCallID: tc.ID,
					Content:    errContent,
				})
				if err := h.appendSessionEvent(ctx, NewToolSessionEvent(tc.ID, errContent)); err != nil {
					h.activeInvocationID = ""
					return nil, err
				}
				continue
			}
			if tc.Name == ToolProposeWritePlan {
				content, proposeRes, proposeErr := h.handleProposeWritePlan(input)
				summary.Err = proposeErr
				if proposeRes != nil {
					summary.Result = proposeRes
				}
				toolSummaries = append(toolSummaries, summary)
				toolInputs = append(toolInputs, input)
				toolMsg := ChatMessage{Role: ChatRoleTool, ToolCallID: tc.ID, Content: content}
				h.session.Messages = append(h.session.Messages, toolMsg)
				if err := h.appendSessionEvent(ctx, NewToolSessionEvent(tc.ID, content)); err != nil {
					h.activeInvocationID = ""
					return nil, err
				}
				continue
			}
			if tc.Name == ToolProposeWriteResource || tc.Name == ToolProposePatientCreate {
				content, proposeRes, proposeErr := h.handleProposeWrite(input, tc.Name)
				summary.Err = proposeErr
				if proposeRes != nil {
					summary.Result = proposeRes
				}
				toolSummaries = append(toolSummaries, summary)
				toolInputs = append(toolInputs, input)
				toolMsg := ChatMessage{Role: ChatRoleTool, ToolCallID: tc.ID, Content: content}
				h.session.Messages = append(h.session.Messages, toolMsg)
				if err := h.appendSessionEvent(ctx, NewToolSessionEvent(tc.ID, content)); err != nil {
					h.activeInvocationID = ""
					return nil, err
				}
				continue
			}
			toolReq := ToolRequest{
				ToolName:       tc.Name,
				Actor:          h.cfg.Actor,
				TenantID:       h.cfg.TenantID,
				Subject:        h.cfg.Subject,
				Input:          input,
				ConversationID: h.session.ConversationID,
			}
			var res *ToolResult
			var execErr error
			if IsWriteTool(tc.Name) {
				if confirmErr := h.confirmBeforeWriteToolInput(ctx, tc.Name, input, CommitWriteOptions{}); confirmErr != nil {
					summary.Err = confirmErr
					toolSummaries = append(toolSummaries, summary)
					toolInputs = append(toolInputs, input)
					errContent := toolErrorContent(confirmErr)
					h.session.Messages = append(h.session.Messages, ChatMessage{
						Role: ChatRoleTool, ToolCallID: tc.ID, Content: errContent,
					})
					if err := h.appendSessionEvent(ctx, NewToolSessionEvent(tc.ID, errContent)); err != nil {
						h.activeInvocationID = ""
						return nil, err
					}
					continue
				}
				res, execErr = h.executeHarnessWriteTool(ctx, tc.Name, input)
			} else if tc.Name == ToolSearchFhirResources && h.groundingConfig().PreflightSearchPolicy {
				if preflightErr := PreflightSearchPolicy(ctx, h.cfg.Executor.Policy(), toolReq, input); preflightErr != nil {
					summary.Err = preflightErr
					toolSummaries = append(toolSummaries, summary)
					toolInputs = append(toolInputs, input)
					errContent := toolErrorContent(preflightErr)
					h.session.Messages = append(h.session.Messages, ChatMessage{
						Role: ChatRoleTool, ToolCallID: tc.ID, Content: errContent,
					})
					if err := h.appendSessionEvent(ctx, NewToolSessionEvent(tc.ID, errContent)); err != nil {
						h.activeInvocationID = ""
						return nil, err
					}
					continue
				}
				res, execErr = h.cfg.Executor.ExecuteTool(ctx, toolReq)
			} else {
				res, execErr = h.cfg.Executor.ExecuteTool(ctx, toolReq)
			}
			summary.Result = res
			summary.Err = execErr
			toolSummaries = append(toolSummaries, summary)
			toolInputs = append(toolInputs, input)

			content := h.formatToolResultContent(tc.Name, res, execErr)
			h.session.Messages = append(h.session.Messages, ChatMessage{
				Role:       ChatRoleTool,
				ToolCallID: tc.ID,
				Content:    content,
			})
			if err := h.appendSessionEvent(ctx, NewToolSessionEvent(tc.ID, content)); err != nil {
				h.activeInvocationID = ""
				return nil, err
			}
		}
		_ = h.maybeCompactSession(ctx, tools)
	}

	h.activeInvocationID = ""
	return nil, fmt.Errorf("ai: exceeded max tool rounds (%d)", h.cfg.MaxToolRounds)
}

func (h *Harness) toolCallProtocol() ToolCallProtocol {
	if h == nil || h.cfg.ToolCallProtocol == "" {
		return ToolCallProtocolNative
	}
	return h.cfg.ToolCallProtocol
}

func (h *Harness) nativeToolsForModel(tools []ChatTool) []ChatTool {
	switch h.toolCallProtocol() {
	case ToolCallProtocolPromptJSON:
		return nil
	default:
		return tools
	}
}

func (h *Harness) systemPromptForModel(tools []ChatTool) string {
	switch h.toolCallProtocol() {
	case ToolCallProtocolPromptJSON, ToolCallProtocolBoth:
		return AppendPromptJSONToolInstructions(h.cfg.SystemPrompt, tools)
	default:
		return h.cfg.SystemPrompt
	}
}

func (h *Harness) resolveToolCalls(resp *ChatResponse, tools []ChatTool) ([]ChatToolCall, error) {
	if resp == nil {
		return nil, nil
	}
	switch h.toolCallProtocol() {
	case ToolCallProtocolNative:
		return resp.ToolCalls, nil
	case ToolCallProtocolPromptJSON:
		return ParsePromptToolCalls(resp.Content, tools)
	case ToolCallProtocolBoth:
		if len(resp.ToolCalls) > 0 {
			return resp.ToolCalls, nil
		}
		return ParsePromptToolCalls(resp.Content, tools)
	default:
		return resp.ToolCalls, nil
	}
}

func (h *Harness) chatTools() []ChatTool {
	descriptors := FilterToolDescriptorsForHarness(h.cfg.Executor.ToolDescriptors(), h.cfg.BlockDirectWriteTools)
	if h.cfg.EnableProposeWriteHelper || h.cfg.EnablePatientCreateHelper {
		descriptors = append(descriptors, ProposeWriteResourceToolDescriptor())
	}
	if h.cfg.EnableProposeWritePlanHelper {
		descriptors = append(descriptors, ProposeWritePlanToolDescriptor())
	}
	return ChatToolsFromDescriptors(descriptors)
}

func (h *Harness) ensureConversationID() {
	if h == nil || h.session.ConversationID != "" || !h.cfg.AutoConversationID {
		return
	}
	h.session.ConversationID = uuid.NewString()
}

func (h *Harness) buildChatResult(invocationID, answer string, toolSummaries []HarnessToolResult) *ChatResult {
	return &ChatResult{
		InvocationID:     invocationID,
		Answer:           answer,
		Messages:         append([]ChatMessage(nil), h.session.Messages...),
		ToolResults:      toolSummaries,
		Citations:        MergeHarnessCitations(nil, toolSummaries),
		PendingApprovals: pendingApprovalsFromResults(toolSummaries),
	}
}

func pendingApprovalsFromResults(toolSummaries []HarnessToolResult) []HarnessPendingApproval {
	var out []HarnessPendingApproval
	for _, summary := range toolSummaries {
		if summary.Result == nil || !summary.Result.ApprovalRequired {
			continue
		}
		out = append(out, HarnessPendingApproval{
			ToolName: summary.ToolName,
			Token:    summary.Result.ApprovalToken,
			Preview:  summary.Result.Data,
		})
	}
	return out
}

func (h *Harness) handleProposeWrite(input map[string]any, toolName string) (string, *ToolResult, error) {
	var writeInput map[string]any
	var execTool string
	var err error
	if toolName == ToolProposePatientCreate {
		patient, perr := PatientCreateDraftFromMap(input)
		if perr != nil {
			return toolErrorContent(perr), nil, perr
		}
		writeInput, err = patient.ToCreateFhirResourceInput()
		execTool = ToolCreateFhirResource
	} else {
		draft, mapErr := ResourceWriteDraftFromMap(input)
		if mapErr != nil {
			return toolErrorContent(mapErr), nil, mapErr
		}
		execTool, writeInput, err = draft.ToExecutorToolInput()
	}
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	payload := map[string]any{
		"status":     "proposal",
		"tool":       execTool,
		"input":      writeInput,
		"commitHint": "Call Harness.CommitWrite with the same structured draft; the host commits via transaction bundle and adds Provenance when configured.",
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	return string(out), &ToolResult{
		ToolName: execTool,
		Data:     payload,
		Context:  string(out),
	}, nil
}

func (h *Harness) handleProposeWritePlan(input map[string]any) (string, *ToolResult, error) {
	plan, err := ResourceWritePlanFromMap(input)
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	txInput, err := plan.ToTransactionInput()
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	payload := map[string]any{
		"status":     "proposal",
		"tool":       ToolExecuteFhirTransaction,
		"input":      txInput,
		"entryCount": len(plan.Entries),
		"commitHint": "Call Harness.CommitWritePlan with the same entries; the host runs a transaction bundle and adds Provenance when configured.",
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolErrorContent(err), nil, err
	}
	return string(out), &ToolResult{
		ToolName: ToolProposeWritePlan,
		Data:     payload,
		Context:  string(out),
	}, nil
}

// executeHarnessWriteTool commits writes through a transaction bundle (Provenance appended by executor attribution).
func (h *Harness) executeHarnessWriteTool(ctx context.Context, toolName string, input map[string]any) (*ToolResult, error) {
	var plan ResourceWritePlan
	var err error
	switch toolName {
	case ToolCreateFhirResource, ToolUpdateFhirResource:
		draft, err := draftFromWriteToolInput(toolName, input)
		if err != nil {
			return nil, err
		}
		plan = ResourceWritePlan{Entries: []ResourceWriteDraft{draft}}
	case ToolExecuteFhirTransaction:
		plan, err = ResourceWritePlanFromTransactionInput(input)
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, toolName)
	}
	if err != nil {
		return nil, err
	}
	return h.executeCommitPlan(ctx, plan, CommitWriteOptions{SkipHostConfirm: true})
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
