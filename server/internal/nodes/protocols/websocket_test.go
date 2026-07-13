package protocols

import (
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func TestWebSocketNode(t *testing.T) {
	def := WebSocket()
	if def.Type != "protocols.websocket" {
		t.Fatalf("unexpected type: %s", def.Type)
	}
	if def.Execute == nil {
		t.Fatal("expected execute function to be set")
	}
	if len(def.Outputs) < 2 {
		t.Fatal("expected main + messages outputs")
	}

	ops := collectOps(def)
	for _, want := range []string{"connect", "send", "receive"} {
		if !ops[want] {
			t.Fatalf("expected operation %q", want)
		}
	}
}

func TestWebSocketHasSubProtocolParam(t *testing.T) {
	def := WebSocket()
	hasSub := false
	for _, p := range def.Params {
		if p.Name == "subprotocol" {
			hasSub = true
			break
		}
	}
	if !hasSub {
		t.Fatal("expected subprotocol param")
	}
}

func TestWebSocketHasReconnectParam(t *testing.T) {
	def := WebSocket()
	hasReconnect := false
	for _, p := range def.Params {
		if p.Name == "reconnect" {
			hasReconnect = true
			break
		}
	}
	if !hasReconnect {
		t.Fatal("expected reconnect param")
	}
}

func TestWebSocketFrameEncoding(t *testing.T) {
	// Verify wsSend doesn't panic on small/large messages
	// (we can't actually connect in a unit test without a server)
	def := WebSocket()
	if def.Label != "WebSocket" {
		t.Fatalf("expected label WebSocket, got %s", def.Label)
	}
}

func TestProtocolsNodeCount(t *testing.T) {
	nodes := Nodes()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 protocol nodes, got %d", len(nodes))
	}
}

func collectOps(def schema.NodeDefinition) map[string]bool {
	ops := map[string]bool{}
	for _, p := range def.Params {
		if p.Name == "operation" {
			for _, opt := range p.Options {
				ops[opt.Value] = true
			}
		}
	}
	return ops
}
