// Package registry holds the node definitions the engine executes and the canvas
// renders. It is the spine: UI and engine both read from the same registrations.
package registry

import (
	"context"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Registry is an in-memory map of node type -> definition.
type Registry struct {
	defs map[string]schema.NodeDefinition
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{defs: map[string]schema.NodeDefinition{}}
}

// Register adds definitions (chainable). Later registrations override earlier
// ones of the same type.
func (r *Registry) Register(defs ...schema.NodeDefinition) *Registry {
	for _, d := range defs {
		r.defs[d.Type] = d
	}
	return r
}

// Get returns a definition by type.
func (r *Registry) Get(t string) (schema.NodeDefinition, bool) {
	d, ok := r.defs[t]
	return d, ok
}

// Has reports whether a type is registered.
func (r *Registry) Has(t string) bool {
	_, ok := r.defs[t]
	return ok
}

// All returns every registered definition.
func (r *Registry) All() []schema.NodeDefinition {
	out := make([]schema.NodeDefinition, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	return out
}

// LoadOptions calls the node's LoadOptions function if registered. Returns nil
// if the node type has no LoadOptions set. Used by GET /api/nodes/{type}/options.
func (r *Registry) LoadOptions(ctx context.Context, nodeType, param, query, credentialID string) ([]schema.ParamOption, error) {
	d, ok := r.defs[nodeType]
	if !ok || d.LoadOptions == nil {
		return nil, nil
	}
	return d.LoadOptions(ctx, param, query, credentialID)
}

// Descriptors returns the serializable metadata for every node (for GET /api/nodes).
func (r *Registry) Descriptors() []schema.NodeDescriptor {
	out := make([]schema.NodeDescriptor, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d.Descriptor())
	}
	return out
}
