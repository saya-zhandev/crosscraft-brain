// Package protocols provides generic protocol integration nodes: GraphQL,
// NATS, and WebSocket. Nodes mix declarative REST (for HTTP-based protocols)
// and native implementations where protocol-level client libraries are required.
package protocols

import (
	"fmt"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Nodes returns the full generic-protocol node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		GraphQL(),
		NATS(),
		WebSocket(),
	}
}

// paramStr extracts a string param, returning def if the param is missing,
// nil, or empty.
func paramStr(params map[string]any, name, def string) string {
	if v, ok := params[name]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		return fmt.Sprint(v)
	}
	return def
}
