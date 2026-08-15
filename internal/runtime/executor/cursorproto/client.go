package cursorproto

// AgentClientMessage is the top-level full-duplex frame the client sends to
// AgentService.Run. Only run_request is used by this client.
type AgentClientMessage struct {
	RunRequest       *AgentRunRequest
	KvClientMessage  *KvClientMessage
	ExecClientMessage *ExecClientMessage
}

func (m *AgentClientMessage) encode() []byte {
	e := &encoder{}
	e.message(1, m.RunRequest)
	e.message(3, m.KvClientMessage)
	e.message(2, m.ExecClientMessage)
	return e.buf
}

// Encode serializes the message.
func (m *AgentClientMessage) Encode() []byte { return m.encode() }

// KvClientMessage answers server KV requests (blob storage protocol).
type KvClientMessage struct {
	ID      uint32
	// exactly one of the results below
	GetBlobData []byte
	HaveGetBlob bool
	SetBlobDone bool
}

func (m *KvClientMessage) encode() []byte {
	e := &encoder{}
	if m.ID != 0 {
		e.uint32(1, m.ID)
	}
	if m.HaveGetBlob {
		res := &encoder{}
		if len(m.GetBlobData) > 0 {
			res.bytes(1, m.GetBlobData)
		}
		e.message(2, &rawMarshaler{res.buf})
	} else if m.SetBlobDone {
		e.message(3, &rawMarshaler{})
	}
	return e.buf
}

type rawMarshaler struct{ raw []byte }

func (r *rawMarshaler) encode() []byte { return r.raw }

// AgentRunRequest drives a full agent turn.
type AgentRunRequest struct {
	ConversationState  *ConversationStateStructure
	Action             *ConversationAction
	ModelDetails       *ModelDetails
	RequestedModel     *RequestedModel
	McpTools           *McpTools
	ConversationID     string
	CustomSystemPrompt string
}

// ExecClientMessage answers server exec requests. V1 only implements
// requestContextResult; other exec variants are rejected server-side via an
// error result so the turn fails fast instead of hanging.
type ExecClientMessage struct {
	ID  uint32
	// RequestContextResult carries the client environment + tools when set.
	RequestContextResult *RequestContextFull
	// ErrorMessage reports a failed exec variant with the given message.
	ErrorMessage string
}

func (m *ExecClientMessage) encode() []byte {
	e := &encoder{}
	if m.ID != 0 {
		e.uint32(1, m.ID)
	}
	if m.RequestContextResult != nil {
		e.message(10, m.RequestContextResult)
	} else if m.ErrorMessage != "" {
		res := &encoder{}
		res.string(2, m.ErrorMessage)
		e.message(10, &rawMarshaler{res.buf})
	}
	return e.buf
}

// RequestContextFull is ExecClientMessage.request_context_result(10):
// Result{success=1{RequestContext}} | {error=2{error string}}.
type RequestContextFull struct {
	Context *RequestContext
}

func (m *RequestContextFull) encode() []byte {
	e := &encoder{}
	if m.Context != nil {
		// RequestContextResult.success(1) { RequestContextSuccess.request_context(1) { ctx } }
		inner := &encoder{}
		inner.message(1, m.Context)
		e.message(1, &rawMarshaler{inner.buf})
	}
	return e.buf
}
// AgentRunRequest drives a full agent turn.

func (m *AgentRunRequest) encode() []byte {
	e := &encoder{}
	e.message(1, m.ConversationState)
	e.message(2, m.Action)
	e.message(3, m.ModelDetails)
	e.message(9, m.RequestedModel)
	e.message(4, m.McpTools)
	if m.ConversationID != "" {
		e.string(5, m.ConversationID)
	}
	if m.CustomSystemPrompt != "" {
		e.string(8, m.CustomSystemPrompt)
	}
	return e.buf
}

// ConversationStateStructure carries conversation history. rootPromptMessages
// holds the flattened prompt history as serialized PromptMessage blobs.
type ConversationStateStructure struct {
	RootPromptMessages [][]byte
	Turns              [][]byte
	Mode               int32
}

func (m *ConversationStateStructure) encode() []byte {
	e := &encoder{}
	for _, raw := range m.RootPromptMessages {
		e.bytes(1, raw)
	}
	for _, raw := range m.Turns {
		e.bytes(8, raw)
	}
	if m.Mode != 0 {
		e.int32(10, m.Mode)
	}
	return e.buf
}

// ConversationMode values for ConversationStateStructure.mode.
const (
	ConversationModeChat  = 1
	ConversationModeAgent = 2
)

// PromptMessage is a single serialized entry inside
// ConversationStateStructure.root_prompt_messages_json. The Cursor CLI stores
// these as JSON envelopes; we mirror the observed shape:
// {"role":"user","content":"..."} / {"role":"assistant","content":"..."}.
type PromptMessage struct {
	Role    string
	Content string
}

// EncodeJSON renders the prompt message envelope.
func (p *PromptMessage) EncodeJSON() []byte {
	e := &encoder{}
	role := p.Role
	if role == "" {
		role = "user"
	}
	content := escapeJSONString(p.Content)
	out := make([]byte, 0, len(role)+len(content)+24)
	out = append(out, `{"role":"`...)
	out = append(out, role...)
	out = append(out, `","content":`...)
	out = append(out, content...)
	out = append(out, '}')
	_ = e
	return out
}

// escapeJSONString quotes and escapes s as a JSON string literal.
func escapeJSONString(s string) []byte {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			out = append(out, '\\', '"')
		case c == '\\':
			out = append(out, '\\', '\\')
		case c == '\n':
			out = append(out, '\\', 'n')
		case c == '\r':
			out = append(out, '\\', 'r')
		case c == '\t':
			out = append(out, '\\', 't')
		case c < 0x20:
			out = append(out, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		default:
			out = append(out, c)
		}
	}
	out = append(out, '"')
	return out
}

var hexDigits = [...]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}

// ConversationAction selects what the agent should do this turn.
type ConversationAction struct {
	UserMessageAction *UserMessageAction
	ResumeAction      *ResumeAction
}

func (m *ConversationAction) encode() []byte {
	e := &encoder{}
	e.message(1, m.UserMessageAction)
	e.message(2, m.ResumeAction)
	return e.buf
}

// ResumeAction resumes generation (used for tool-result turns).
type ResumeAction struct{}

func (m *ResumeAction) encode() []byte { return nil }

// UserMessageAction delivers a new user message.
type UserMessageAction struct {
	UserMessage   *UserMessage
	RequestContext *RequestContext
}

func (m *UserMessageAction) encode() []byte {
	e := &encoder{}
	e.message(1, m.UserMessage)
	e.message(2, m.RequestContext)
	return e.buf
}

// RequestContext carries the client environment the agent runs in. Cursor's
// backend gates turn execution on its presence for CLI clients.
type RequestContext struct {
	Env   *RequestContextEnv
	Tools []*McpToolDefinition
}

func (m *RequestContext) encode() []byte {
	e := &encoder{}
	e.message(4, m.Env)
	for _, tool := range m.Tools {
		e.message(7, tool)
	}
	return e.buf
}

// RequestContextEnv describes the local execution environment.
type RequestContextEnv struct {
	OSVersion      string
	WorkspacePaths string
	Shell          string
	TimeZone       string
	ProjectFolder  string
}

func (m *RequestContextEnv) encode() []byte {
	e := &encoder{}
	if m.OSVersion != "" {
		e.string(1, m.OSVersion)
	}
	if m.WorkspacePaths != "" {
		e.string(2, m.WorkspacePaths)
	}
	if m.Shell != "" {
		e.string(3, m.Shell)
	}
	if m.TimeZone != "" {
		e.string(10, m.TimeZone)
	}
	if m.ProjectFolder != "" {
		e.string(11, m.ProjectFolder)
	}
	return e.buf
}

// UserMessage carries the user prompt text.
type UserMessage struct {
	Text      string
	MessageID string
	Mode      int32
}

func (m *UserMessage) encode() []byte {
	e := &encoder{}
	if m.Text != "" {
		e.string(1, m.Text)
	}
	if m.MessageID != "" {
		e.string(2, m.MessageID)
	}
	if m.Mode != 0 {
		e.int32(4, m.Mode)
	}
	return e.buf
}

// RequestedModel selects the model for this run.
type RequestedModel struct {
	ModelID string
	MaxMode bool
}

func (m *RequestedModel) encode() []byte {
	e := &encoder{}
	if m.ModelID != "" {
		e.string(1, m.ModelID)
	}
	if m.MaxMode {
		e.bool(2, m.MaxMode)
	}
	return e.buf
}

// ModelDetails mirrors the model catalog entry.
type ModelDetails struct {
	ModelID        string
	DisplayModelID string
	DisplayName    string
	MaxMode        bool
}

func (m *ModelDetails) encode() []byte {
	e := &encoder{}
	if m.ModelID != "" {
		e.string(1, m.ModelID)
	}
	if m.DisplayModelID != "" {
		e.string(3, m.DisplayModelID)
	}
	if m.DisplayName != "" {
		e.string(4, m.DisplayName)
	}
	if m.MaxMode {
		e.bool(7, m.MaxMode)
	}
	return e.buf
}

// McpTools declares function tools the model may call.
type McpTools struct {
	Tools []*McpToolDefinition
}

func (m *McpTools) encode() []byte {
	e := &encoder{}
	for _, t := range m.Tools {
		e.message(1, t)
	}
	return e.buf
}

// McpToolDefinition is a single function tool (JSON schema in InputSchemaJSON).
type McpToolDefinition struct {
	Name            string
	Description     string
	InputSchemaJSON string
}

func (m *McpToolDefinition) encode() []byte {
	e := &encoder{}
	if m.Name != "" {
		e.string(1, m.Name)
	}
	if m.Description != "" {
		e.string(2, m.Description)
	}
	if m.InputSchemaJSON != "" {
		e.string(6, m.InputSchemaJSON)
	}
	return e.buf
}

// AgentConversationTurnStructure is one serialized entry of
// ConversationStateStructure.turns: a user message plus the assistant steps
// that followed it.
type AgentConversationTurnStructure struct {
	UserMessage []byte   // serialized UserMessage
	Steps       [][]byte // serialized ConversationStep
	RequestID   string
}

func (m *AgentConversationTurnStructure) encode() []byte {
	e := &encoder{}
	if len(m.UserMessage) > 0 {
		e.bytes(1, m.UserMessage)
	}
	for _, s := range m.Steps {
		e.bytes(2, s)
	}
	if m.RequestID != "" {
		e.string(3, m.RequestID)
	}
	return e.buf
}

// EncodeTurn wraps an agent turn into the ConversationTurnStructure oneof.
func EncodeTurn(agent *AgentConversationTurnStructure) []byte {
	e := &encoder{}
	e.message(1, agent)
	return e.buf
}

// ConversationStep is one assistant step inside a turn.
type ConversationStep struct {
	AssistantText  string
	ThinkingText   string
}

func (m *ConversationStep) encode() []byte {
	e := &encoder{}
	if m.AssistantText != "" {
		e.message(1, &AssistantMessage{Text: m.AssistantText})
	}
	if m.ThinkingText != "" {
		e.message(3, &ThinkingMessage{Text: m.ThinkingText})
	}
	return e.buf
}

// AssistantMessage is a plain assistant text step.
type AssistantMessage struct {
	Text string
}

func (m *AssistantMessage) encode() []byte {
	e := &encoder{}
	if m.Text != "" {
		e.string(1, m.Text)
	}
	return e.buf
}

// ThinkingMessage is a reasoning trace step.
type ThinkingMessage struct {
	Text       string
	DurationMS uint32
}

func (m *ThinkingMessage) encode() []byte {
	e := &encoder{}
	if m.Text != "" {
		e.string(1, m.Text)
	}
	if m.DurationMS > 0 {
		e.uint32(2, m.DurationMS)
	}
	return e.buf
}

// Encode serializes the message (public wrapper for turn construction).
func (m *UserMessage) Encode() []byte { return m.encode() }

// Encode serializes the step.
func (m *ConversationStep) Encode() []byte { return m.encode() }
