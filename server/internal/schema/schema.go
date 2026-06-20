// Package schema is the Go mirror of @crosscraft/schema — the wire contract the
// canvas, engine and API all speak. JSON tags reproduce the TypeScript shapes
// byte-for-byte so the existing frontend works unchanged.
package schema

import "net/http"

// DefaultPort is the implicit input/output port id.
const DefaultPort = "main"

// ---------------------------------------------------------------------------
// Workflow graph
// ---------------------------------------------------------------------------

// BinaryRef references binary data kept out of the JSON item.
type BinaryRef struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	FileName string `json:"fileName,omitempty"`
}

// Item is one JSON object flowing on a connection. Nodes emit/consume []Item.
type Item struct {
	JSON   map[string]any       `json:"json"`
	Binary map[string]BinaryRef `json:"binary,omitempty"`
}

// Position is a node's canvas coordinate.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WFNode is a node instance in a saved workflow graph.
type WFNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Params   map[string]any `json:"params"`
	Position Position       `json:"position"`
	Name     string         `json:"name,omitempty"`
}

// WFEdge connects a source output port to a target input port.
type WFEdge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceHandle string `json:"sourceHandle,omitempty"`
	Target       string `json:"target"`
	TargetHandle string `json:"targetHandle,omitempty"`
}

// Workflow is the persisted graph the canvas saves and the engine runs.
type Workflow struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Active   bool           `json:"active"`
	Nodes    []WFNode       `json:"nodes"`
	Edges    []WFEdge       `json:"edges"`
	Settings map[string]any `json:"settings,omitempty"`
}

// ---------------------------------------------------------------------------
// Node authoring contract
// ---------------------------------------------------------------------------

// Port is an input/output handle on a node.
type Port struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// ParamOption is a choice for a select param.
type ParamOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ShowWhen conditionally reveals a param based on another param's value.
type ShowWhen struct {
	Param  string `json:"param"`
	Equals []any  `json:"equals"`
}

// ParamSchema declares one configurable field of a node.
type ParamSchema struct {
	Name           string        `json:"name"`
	Label          string        `json:"label"`
	Type           string        `json:"type"` // string|number|boolean|select|json|expression|credential
	Required       bool          `json:"required,omitempty"`
	Default        any           `json:"default,omitempty"`
	Placeholder    string        `json:"placeholder,omitempty"`
	Description    string        `json:"description,omitempty"`
	Options        []ParamOption `json:"options,omitempty"`
	CredentialType string        `json:"credentialType,omitempty"`
	ShowWhen       *ShowWhen     `json:"showWhen,omitempty"`
}

// RespondSpec is the optional response a suspending node returns to its caller.
type RespondSpec struct {
	Status int `json:"status,omitempty"`
	Body   any `json:"body,omitempty"`
}

// SuspendRequest is returned by a node that pauses the run (durable wait).
type SuspendRequest struct {
	Kind    string       `json:"kind"` // "webhook"
	Respond *RespondSpec `json:"respond,omitempty"`
}

// NodeResult is what a node's Execute returns: items per output port, OR a
// suspend request. Exactly one of the fields is set.
type NodeResult struct {
	Outputs map[string][]Item
	Suspend *SuspendRequest
}

// ExecIDs identifies the current run context.
type ExecIDs struct {
	WorkflowID  string
	ExecutionID string
	NodeID      string
}

// ExecContext is handed to a node's Execute. Closures are wired by the engine.
type ExecContext struct {
	Input      []Item
	Params     map[string]any
	RawParam   func(name string) any
	Upstream   func(nodeID string) []Item
	Credential func(paramName string) (map[string]any, error)
	// AuthorizedClient returns an HTTP client authenticated with the given
	// credential param (e.g. an OAuth2 client) — used by REST/integration nodes.
	AuthorizedClient func(paramName string) (*http.Client, error)
	Trigger          []Item
	Log        func(message string, data any)
	First      func() map[string]any
	IDs        ExecIDs
}

// NodeDefinition is the authoring contract: metadata + an Execute func. A fork
// adds NodeDefinitions and registers them; the registry serializes the metadata
// to a NodeDescriptor for the canvas.
type NodeDefinition struct {
	Type        string
	Label       string
	Group       string // trigger|transform|flow|integration|ai
	Icon        string
	Description string
	Inputs      []Port
	Outputs     []Port
	Params      []ParamSchema
	Credentials []string
	IsTrigger   bool
	Execute     func(ctx *ExecContext) (NodeResult, error)
}

// NodeDescriptor is the serializable view of a node (no Execute) for the API.
type NodeDescriptor struct {
	Type        string        `json:"type"`
	Label       string        `json:"label"`
	Group       string        `json:"group"`
	Icon        string        `json:"icon,omitempty"`
	Description string        `json:"description,omitempty"`
	Inputs      []Port        `json:"inputs"`
	Outputs     []Port        `json:"outputs"`
	Params      []ParamSchema `json:"params"`
	Credentials []string      `json:"credentials,omitempty"`
	IsTrigger   bool          `json:"isTrigger,omitempty"`
}

// Descriptor returns the serializable metadata for this node.
func (d NodeDefinition) Descriptor() NodeDescriptor {
	return NodeDescriptor{
		Type:        d.Type,
		Label:       d.Label,
		Group:       d.Group,
		Icon:        d.Icon,
		Description: d.Description,
		Inputs:      d.Inputs,
		Outputs:     d.Outputs,
		Params:      d.Params,
		Credentials: d.Credentials,
		IsTrigger:   d.IsTrigger,
	}
}

// ---------------------------------------------------------------------------
// Execution / monitoring records
// ---------------------------------------------------------------------------

// LogEntry is one structured log line captured into a step record.
type LogEntry struct {
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ExecutionRecord is a run's summary row.
type ExecutionRecord struct {
	ID            string  `json:"id"`
	WorkflowID    string  `json:"workflowId"`
	Status        string  `json:"status"` // running|waiting|success|error
	ResumeToken   *string `json:"resumeToken,omitempty"`
	WaitingNodeID *string `json:"waitingNodeId,omitempty"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    *string `json:"finishedAt,omitempty"`
}

// StepRecord is a single node's I/O record (powers monitoring).
type StepRecord struct {
	ID          string     `json:"id"`
	ExecutionID string     `json:"executionId"`
	NodeID      string     `json:"nodeId"`
	Status      string     `json:"status"` // running|success|error
	Input       []Item     `json:"input"`
	Output      []Item     `json:"output"`
	Logs        []LogEntry `json:"logs,omitempty"`
	Error       *string    `json:"error,omitempty"`
	StartedAt   string     `json:"startedAt"`
	FinishedAt  *string    `json:"finishedAt,omitempty"`
}
