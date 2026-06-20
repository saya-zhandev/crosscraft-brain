// Package scheduler fires workflows whose trigger is core.scheduleTrigger on an
// interval or cron schedule. It polls active workflows on a tick and runs the ones
// that are due via the engine's bounded worker pool. Last-fire times are tracked
// in memory (a workflow is baselined when first seen, then fires on its cadence);
// durable schedule state across restarts is a follow-up.
package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	cron "github.com/robfig/cron/v3"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/store"
)

const triggerType = "core.scheduleTrigger"

// Scheduler periodically runs due scheduled workflows.
type Scheduler struct {
	store *store.Store
	eng   *engine.Engine
	tick  time.Duration
	mu    sync.Mutex
	last  map[string]time.Time
}

// New builds a scheduler (15s tick — sub-minute interval resolution is limited by it).
func New(st *store.Store, eng *engine.Engine) *Scheduler {
	return &Scheduler{store: st, eng: eng, tick: 15 * time.Second, last: map[string]time.Time{}}
}

// Start launches the polling loop until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(s.tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.fireDue(ctx, time.Now())
			}
		}
	}()
}

func (s *Scheduler) fireDue(ctx context.Context, now time.Time) {
	wfs, err := s.store.ListActiveWorkflows(ctx)
	if err != nil {
		log.Printf("scheduler: list active workflows: %v", err)
		return
	}
	for i := range wfs {
		wf := &wfs[i]
		trig := scheduleTrigger(wf)
		if trig == nil {
			continue
		}
		last, seen := s.getLast(wf.ID)
		if !seen {
			s.setLast(wf.ID, now) // baseline; fire on a later tick
			continue
		}
		if due(trig.Params, last, now) {
			s.setLast(wf.ID, now)
			payload := []schema.Item{{JSON: map[string]any{
				"timestamp": now.UTC().Format(time.RFC3339),
				"unix":      now.Unix(),
				"mode":      "schedule",
			}}}
			if _, err := s.eng.Run(ctx, wf, payload); err != nil {
				log.Printf("scheduler: run %s: %v", wf.ID, err)
			}
		}
	}
}

func scheduleTrigger(wf *schema.Workflow) *schema.WFNode {
	for i := range wf.Nodes {
		if wf.Nodes[i].Type == triggerType {
			return &wf.Nodes[i]
		}
	}
	return nil
}

// due reports whether a scheduled workflow should fire now given its last fire time.
func due(params map[string]any, last, now time.Time) bool {
	if str(params["mode"], "interval") == "cron" {
		sched, err := cron.ParseStandard(str(params["cron"], ""))
		if err != nil {
			return false
		}
		return !sched.Next(last).After(now)
	}
	d := durationOf(asInt(params["amount"], 0), str(params["unit"], "minutes"))
	return d > 0 && now.Sub(last) >= d
}

func (s *Scheduler) getLast(id string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.last[id]
	return t, ok
}

func (s *Scheduler) setLast(id string, t time.Time) {
	s.mu.Lock()
	s.last[id] = t
	s.mu.Unlock()
}

func str(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func asInt(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return def
}

func durationOf(amount int, unit string) time.Duration {
	switch unit {
	case "seconds":
		return time.Duration(amount) * time.Second
	case "hours":
		return time.Duration(amount) * time.Hour
	case "days":
		return time.Duration(amount) * 24 * time.Hour
	default:
		return time.Duration(amount) * time.Minute
	}
}
