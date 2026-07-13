// Command benchserver is a standalone pprof-enabled HTTP server that exercises
// crosscraft-brain internals under controlled synthetic load. It does NOT require
// Postgres — all work targets the in-process registry, schema types, and node
// definitions directly, giving a clean CPU/memory profile of the application layer.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/id"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/llm"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/ai"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/core"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/database"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/dev"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/protocols"
)

// Global counters for the stats endpoint.
var (
	execCount    atomic.Int64
	jsonMarshalN atomic.Int64
)

// ---- Load Generators ----

// cpuBurn continuously encodes/decodes workflow-like JSON objects, exercising
// the json and schema packages under load.
func cpuBurn(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
		}
		obj := map[string]any{
			"id":   id.New(),
			"name": fmt.Sprintf("bench-%d", time.Now().UnixNano()),
			"nodes": []map[string]any{
				{"id": "n1", "type": "core.http", "params": map[string]any{"url": "https://example.com", "method": "GET"}},
				{"id": "n2", "type": "core.if", "params": map[string]any{"condition": "{{ $json.status == 200 }}"}},
				{"id": "n3", "type": "core.set", "params": map[string]any{"key": "result", "value": "{{ $json.body }}"}},
				{"id": "n4", "type": "core.http", "params": map[string]any{"url": "https://api.example.com/data", "method": "POST"}},
				{"id": "n5", "type": "core.extract", "params": map[string]any{"path": "$.items[*]", "from": "body"}},
				{"id": "n6", "type": "core.loop", "params": map[string]any{"iterations": 10}},
				{"id": "n7", "type": "core.spreadsheet", "params": map[string]any{"data": "{{ $json }}"}},
				{"id": "n8", "type": "core.markdown", "params": map[string]any{"template": "# Report\n\n{{ range $json.items }}"}},
			},
			"edges": []map[string]any{
				{"source": "n1", "target": "n2"},
				{"source": "n2", "target": "n3", "sourceHandle": "true"},
				{"source": "n2", "target": "n4", "sourceHandle": "false"},
				{"source": "n3", "target": "n5"},
				{"source": "n5", "target": "n6"},
				{"source": "n6", "target": "n7"},
				{"source": "n7", "target": "n8"},
			},
			"settings": map[string]any{"maxRetries": 3, "timeout": 30},
		}
		b, _ := json.Marshal(obj)
		jsonMarshalN.Add(1)
		_ = b

		// Unmarshal back to exercise the allocator
		var back map[string]any
		json.Unmarshal(b, &back)
		runtime.KeepAlive(back)
	}
}

// allocChurn rapidly allocates strings — stresses GC + heap profiler.
func allocChurn(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
		}
		var sink []string
		for i := 0; i < 500; i++ {
			s := fmt.Sprintf("node-%s-%d-%x-%d", id.New(), time.Now().UnixNano(), i, i*1337)
			sink = append(sink, s)
		}
		// Hold briefly then release
		time.Sleep(1 * time.Millisecond)
		runtime.KeepAlive(sink)
	}
}

// goroutineChurn creates and destroys goroutines in waves.
func goroutineChurn(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
		}
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				// Simulate a short-lived handler goroutine
				data := map[string]any{"index": n, "ts": time.Now().UnixNano()}
				b, _ := json.Marshal(data)
				_ = b
				time.Sleep(time.Duration(n%10) * time.Millisecond)
			}(i)
		}
		wg.Wait()
		time.Sleep(100 * time.Millisecond)
	}
}

// periodicExec simulates workflow execution by walking node descriptors,
// calling param resolution, and allocating result structures.
func periodicExec(reg *registry.Registry, done <-chan struct{}) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			execCount.Add(1)
			descs := reg.Descriptors()
			// Walk all nodes, marshal their definitions (exercises schema types)
			for _, d := range descs {
				b, _ := json.Marshal(d)
				_ = b
				// Exercise param option iteration
				for _, p := range d.Params {
					_ = p.Name
					_ = p.Label
					_ = p.Type
					for _, opt := range p.Options {
						_ = opt.Label
						_ = opt.Value
					}
				}
			}
		}
	}
}

func main() {
	log.SetFlags(log.Ltime)
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║   CrossCraft Brain — Benchmark Server           ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Println("║  pprof endpoints:  http://localhost:8080/debug/  ║")
	fmt.Println("║  Nodes API:        http://localhost:8080/nodes   ║")
	fmt.Println("║  Stats API:        http://localhost:8080/stats   ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()

	// Build the real registry with all node packs — exercises all init paths.
	llmClient := llm.New()
	reg := registry.New().
		Register(core.Nodes...).
		Register(ai.Nodes(llmClient)...).
		Register(database.Nodes()...).
		Register(dev.Nodes()...).
		Register(protocols.Nodes()...)

	log.Printf("Registry loaded: %d node types registered", len(reg.Descriptors()))

	// ---- HTTP Handlers ----
	mux := http.NewServeMux()

	// GET /nodes — exercises registry descriptors + JSON serialization
	mux.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
		descs := reg.Descriptors()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(descs)
	})

	// POST /workflow — exercises JSON decode + schema.Workflow + id generation
	mux.HandleFunc("/workflow", func(w http.ResponseWriter, r *http.Request) {
		var wf schema.Workflow
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &wf)
		if wf.ID == "" {
			wf.ID = id.New()
		}
		if wf.Name == "" {
			wf.Name = "Benchmark"
		}
		if wf.Nodes == nil {
			wf.Nodes = []schema.WFNode{}
		}
		if wf.Edges == nil {
			wf.Edges = []schema.WFEdge{}
		}
		// Marshal back (exercises schema→JSON)
		b, _ := json.Marshal(wf)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})

	// POST /run — simulates a workflow run by parsing and re-serializing
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		execCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		var wf schema.Workflow
		json.Unmarshal(body, &wf)

		// Simulate engine work: walk nodes, resolve params, produce results
		results := make([]map[string]any, 0, len(wf.Nodes))
		for _, node := range wf.Nodes {
			results = append(results, map[string]any{
				"nodeId":   node.ID,
				"nodeType": node.Type,
				"status":   "success",
			})
		}

		resp := map[string]any{
			"executionId": id.New(),
			"status":      "success",
			"steps":       len(results),
			"nodes":       results,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// GET /stats — live benchmark metrics
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		stats := map[string]any{
			"executions":    execCount.Load(),
			"jsonMarshals":  jsonMarshalN.Load(),
			"goroutines":    runtime.NumGoroutine(),
			"heapAllocKB":   ms.HeapAlloc / 1024,
			"heapSysKB":     ms.HeapSys / 1024,
			"heapInuseKB":   ms.HeapInuse / 1024,
			"heapIdleKB":    ms.HeapIdle / 1024,
			"numGC":         ms.NumGC,
			"totalAllocMB":  float64(ms.TotalAlloc) / 1024 / 1024,
			"pauseTotalMs":  float64(ms.PauseTotalNs) / 1e6,
			"numForcedGC":   ms.NumForcedGC,
			"gcCPUPercent":  float64(ms.GCCPUFraction) * 100,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// Re-route /debug/pprof/* to DefaultServeMux where net/http/pprof lives
	mux.HandleFunc("/debug/", func(w http.ResponseWriter, r *http.Request) {
		http.DefaultServeMux.ServeHTTP(w, r)
	})

	// ---- Start background load generators ----
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := ctx.Done()

	// Set GOMAXPROCS to use all available cores
	ncpu := runtime.GOMAXPROCS(0)
	log.Printf("GOMAXPROCS = %d", ncpu)

	// 3x CPU burn goroutines (JSON encode/decode)
	go cpuBurn(done)
	go cpuBurn(done)
	go cpuBurn(done)

	// 2x memory churn goroutines
	go allocChurn(done)
	go allocChurn(done)

	// 1x goroutine wave generator
	go goroutineChurn(done)

	// 1x periodic execution simulator
	go periodicExec(reg, done)

	log.Println("Load generators: 3x CPU burn, 2x alloc churn, goroutine waves, periodic exec")
	log.Println("Synthetic load active — ready for profiling.")
	log.Println()

	// ---- Start HTTP server ----
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Listening on :8080")
		log.Printf("  Profile: go tool pprof -http=:8082 http://localhost:8080/debug/pprof/profile?seconds=30")
		log.Printf("  Heap:    go tool pprof -http=:8083 http://localhost:8080/debug/pprof/heap")
		log.Printf("  Routine: go tool pprof -http=:8084 http://localhost:8080/debug/pprof/goroutine")
		log.Println()
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	log.Println("Done.")
}
