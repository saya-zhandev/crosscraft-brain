// Package triggers provides a generalized polling-trigger framework that nodes
// can use to implement interval-based change detection (new emails, spreadsheet
// row changes, new files, etc.). It manages rate-limiting, deduplication
// cursors, and durable state persistence through ExecContext.State so triggers
// survive across workflow runs and process restarts.
//
// Use case: Every Google/Microsoft trigger node (Sheets rowAdded/rowUpdated,
// Gmail newEmail, Drive newFile, Forms newResponse, etc.) currently implements
// its own polling loop. This package factors out the common logic so future
// trigger nodes only define:
//   1. What to poll (a fetch function)
//   2. How to deduplicate (an ID extractor)
//   3. A cursor extraction function (optional)
//
// Usage pattern inside a node's Execute:
//
//   poll := triggers.New(triggers.Opts{
//       Scope:   "myservice:triggerName:resourceID",
//       Interval: triggers.IntervalSeconds(30),
//   })
//   if !poll.ShouldFire(ctx) {
//       return schema.NodeResult{Outputs: ...empty...}, nil
//   }
//   items := /* fetch new/updated items from the API */
//   emitted := poll.Emit(ctx, items, triggers.WithIDFunc(func(item) string { return item.ID }))
//   return schema.NodeResult{Outputs: map[string][]schema.Item{"main": emitted}}, nil
package triggers

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// PersistentStore is an optional interface for persisting poll state across
// executions. When set on a Poller, the dedup set (SeenIDs), cursor, idle
// count, and poll count survive execution boundaries so items are never
// re-emitted on the next workflow run.
type PersistentStore interface {
	LoadPollState(ctx context.Context, scope string) (*pollState, error)
	SavePollState(ctx context.Context, scope string, state *pollState) error
}

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// IDExtractor returns a unique, stable identifier for a polled item. Used for
// deduplication across polls so the same item is never emitted twice.
type IDExtractor func(item schema.Item) string

// CursorExtractor returns a cursor (position marker) from a poll response.
// For example, Sheets uses the row count; Gmail uses the historyId.
type CursorExtractor func(items []schema.Item) string

// FetchFunc is the callback that polls the external API. It receives the
// previous cursor (empty on first call) and returns the fetched items +
// optionally a new cursor.
type FetchFunc func(ctx *schema.ExecContext, cursor string) (items []schema.Item, newCursor string, err error)

// Opts configures the polling trigger.
type Opts struct {
	// Scope uniquely identifies this polling source. Must be stable across runs.
	// Format: "service:trigger:entity" e.g. "sheets:rowAdded:SHEETID!RANGE"
	Scope string

	// Interval is the minimum time between polls. The trigger will not fire
	// more often than this (rate-limiting).
	Interval time.Duration

	// MinInterval is the absolute floor — Interval is clamped to this.
	MinInterval time.Duration

	// MaxIdlePolls is the number of consecutive polls that return zero items
	// before the interval is doubled (backoff). Helps avoid hammering APIs
	// when nothing changes. Default: 5.
	MaxIdlePolls int
}

// Poller is a stateful polling trigger bound to a single ExecContext.
type Poller struct {
	opts  Opts
	state *pollState // loaded from / persisted to ctx.State
	store PersistentStore
}

// pollState is the serialisable polling state stored in ctx.State under a
// key derived from opts.Scope.
type pollState struct {
	LastPoll    int64    `json:"lastPoll"`    // unix timestamp
	SeenIDs     []string `json:"seenIDs"`     // deduplication set
	Cursor      string   `json:"cursor"`      // position cursor
	IdleCount   int      `json:"idleCount"`   // consecutive idle polls
	PollCount   int      `json:"pollCount"`   // total polls fired
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// DefaultMinInterval is the minimum allowed poll interval.
const DefaultMinInterval = 10 * time.Second

// PollerOption configures a Poller at construction time.
type PollerOption func(*Poller)

// WithPersistentStore sets the persistent store on the Poller so dedup state
// survives across execution boundaries.
func WithPersistentStore(store PersistentStore) PollerOption {
	return func(p *Poller) { p.store = store }
}

// New creates a Poller bound to the given scope and ExecContext. The poller
// reads its previous state from ctx.State and persists updates back.
func New(ctx *schema.ExecContext, opts Opts, pollerOpts ...PollerOption) *Poller {
	if ctx.State == nil {
		ctx.State = map[string]any{}
	}
	if opts.MinInterval <= 0 {
		opts.MinInterval = DefaultMinInterval
	}
	if opts.Interval < opts.MinInterval {
		opts.Interval = opts.MinInterval
	}
	if opts.MaxIdlePolls <= 0 {
		opts.MaxIdlePolls = 5
	}
	ps := loadState(ctx.State, opts.Scope)
	p := &Poller{opts: opts, state: ps}
	for _, o := range pollerOpts {
		o(p)
	}
	// If a persistent store is configured, prefer its durable state over the
	// execution-scoped state (which is fresh on every workflow run).
	if p.store != nil {
		if durable, err := p.store.LoadPollState(context.Background(), opts.Scope); err == nil && durable != nil {
			p.state = durable
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// ShouldFire — rate-limiting check
// ---------------------------------------------------------------------------

// ShouldFire returns true if enough time has passed since the last poll.
// When it returns false, the caller should return an empty result immediately.
func (p *Poller) ShouldFire(ctx *schema.ExecContext) bool {
	interval := p.effectiveInterval()
	if p.state.LastPoll > 0 {
		elapsed := time.Since(time.Unix(p.state.LastPoll, 0))
		if elapsed < interval {
			return false
		}
	}
	p.markFired(ctx)
	return true
}

// effectiveInterval returns the current poll interval, which may be longer
// than the configured interval if idle backoff is active.
func (p *Poller) effectiveInterval() time.Duration {
	base := p.opts.Interval
	if p.state.IdleCount > p.opts.MaxIdlePolls {
		// Double the interval for each idle period beyond the threshold,
		// capped at 30 minutes.
		extra := p.state.IdleCount - p.opts.MaxIdlePolls
		multiplier := int64(1) << extra
		if multiplier > 32 {
			multiplier = 32
		}
		d := base * time.Duration(multiplier)
		if d > 30*time.Minute {
			d = 30 * time.Minute
		}
		return d
	}
	return base
}

// markFired updates the last-poll timestamp and increments the poll counter.
func (p *Poller) markFired(ctx *schema.ExecContext) {
	p.state.LastPoll = time.Now().Unix()
	p.state.PollCount++
	p.savePersistent(ctx)
}

// ---------------------------------------------------------------------------
// Emit — deduplication and cursor management
// ---------------------------------------------------------------------------

// Emit filters items through the deduplication set and returns only new ones.
// It also updates the cursor and resets idle count when new items are found.
//
// Options (passed as variadic EmitOption):
//   - WithIDFunc(fn) — required: extracts a stable ID from each item for dedup.
//   - WithCursor(cursor) — optional: sets the position cursor.
func (p *Poller) Emit(ctx *schema.ExecContext, items []schema.Item, opts ...EmitOption) []schema.Item {
	cfg := emitConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	// If no ID func provided, treat all items as new (no dedup).
	if cfg.idFunc == nil {
		if cfg.cursor != "" {
			p.state.Cursor = cfg.cursor
		}
		if len(items) > 0 {
			p.state.IdleCount = 0
		} else {
			p.state.IdleCount++
		}
		p.savePersistent(ctx)
		return items
	}

	// Build dedup set
	seen := setFromSlice(p.state.SeenIDs)
	var out []schema.Item
	var newIDs []string
	for _, item := range items {
		id := cfg.idFunc(item)
		if id == "" {
			out = append(out, item)
			continue
		}
		if !seen[id] {
			out = append(out, item)
			seen[id] = true
			newIDs = append(newIDs, id)
		}
	}

	// Update state
	if cfg.cursor != "" {
		p.state.Cursor = cfg.cursor
	}
	if len(newIDs) > 0 {
		p.state.IdleCount = 0
		// Persist new IDs (keeping old ones — never GC so re-appearing items
		// that leave and re-enter the time window are not re-emitted).
		for _, id := range newIDs {
			p.state.SeenIDs = append(p.state.SeenIDs, id)
		}
	} else {
		p.state.IdleCount++
	}
	p.savePersistent(ctx)
	return out
}

// ---------------------------------------------------------------------------
// Convenience: Poll helper that combines ShouldFire + Fetch + Emit
// ---------------------------------------------------------------------------

// Poll is a convenience method that combines ShouldFire, a fetch callback,
// and Emit into a single call. If ShouldFire returns false, it returns nil.
// Otherwise it calls fetchFn, deduplicates the results, and returns them.
func (p *Poller) Poll(ctx *schema.ExecContext, fetchFn FetchFunc, opts ...EmitOption) ([]schema.Item, error) {
	if !p.ShouldFire(ctx) {
		return nil, nil
	}
	items, cursor, err := fetchFn(ctx, p.state.Cursor)
	if err != nil {
		return nil, err
	}
	// Merge cursor into options if provided
	allOpts := append([]EmitOption{}, opts...)
	if cursor != "" {
		allOpts = append(allOpts, WithCursor(cursor))
	}
	return p.Emit(ctx, items, allOpts...), nil
}

// ---------------------------------------------------------------------------
// Convenience constructors for common trigger patterns
// ---------------------------------------------------------------------------

// NewSheetsTrigger creates a poller for a Google Sheets trigger.
func NewSheetsTrigger(ctx *schema.ExecContext, spreadsheetID, rangeValue, triggerType string, pollSeconds int) *Poller {
	return New(ctx, Opts{
		Scope:    fmt.Sprintf("sheets:%s:%s:%s", triggerType, spreadsheetID, rangeValue),
		Interval: IntervalSeconds(pollSeconds),
	})
}

// NewGmailTrigger creates a poller for a Gmail trigger.
func NewGmailTrigger(ctx *schema.ExecContext, query, filter string, pollSeconds int) *Poller {
	scope := fmt.Sprintf("gmail:newEmail:%s", hashScope(query+filter))
	return New(ctx, Opts{
		Scope:    scope,
		Interval: IntervalSeconds(pollSeconds),
	})
}

// NewDriveTrigger creates a poller for a Drive trigger.
func NewDriveTrigger(ctx *schema.ExecContext, query, folderID string, pollSeconds int) *Poller {
	scope := fmt.Sprintf("drive:newFile:%s", hashScope(query+"/"+folderID))
	return New(ctx, Opts{
		Scope:    scope,
		Interval: IntervalSeconds(pollSeconds),
	})
}

// NewCalendarTrigger creates a poller for a Calendar trigger.
func NewCalendarTrigger(ctx *schema.ExecContext, calendarID, tMin, tMax string, pollSeconds int) *Poller {
	scope := fmt.Sprintf("calendar:newEvent:%s:%s:%s", calendarID, hashScope(tMin), hashScope(tMax))
	return New(ctx, Opts{
		Scope:    scope,
		Interval: IntervalSeconds(pollSeconds),
	})
}

// NewFormsTrigger creates a poller for a Forms response trigger.
func NewFormsTrigger(ctx *schema.ExecContext, formID string, pollSeconds int) *Poller {
	return New(ctx, Opts{
		Scope:    fmt.Sprintf("forms:newResponse:%s", formID),
		Interval: IntervalSeconds(pollSeconds),
	})
}

// NewGenericTrigger creates a poller scoped to an arbitrary service+entity.
func NewGenericTrigger(ctx *schema.ExecContext, service, entity string, pollSeconds int) *Poller {
	return New(ctx, Opts{
		Scope:    fmt.Sprintf("%s:%s", service, entity),
		Interval: IntervalSeconds(pollSeconds),
	})
}

// ---------------------------------------------------------------------------
// Emit options
// ---------------------------------------------------------------------------

type emitConfig struct {
	idFunc IDExtractor
	cursor string
}

// EmitOption configures the Emit call.
type EmitOption func(*emitConfig)

// WithIDFunc sets the function used to extract stable IDs from items for dedup.
func WithIDFunc(fn IDExtractor) EmitOption {
	return func(c *emitConfig) { c.idFunc = fn }
}

// WithCursor sets the position cursor (e.g. row count, history ID).
func WithCursor(cursor string) EmitOption {
	return func(c *emitConfig) { c.cursor = cursor }
}

// savePersistent writes state to both the execution-scoped ctx.State and the
// optional persistent store, ensuring dedup state survives across executions.
func (p *Poller) savePersistent(ctx *schema.ExecContext) {
	saveState(ctx.State, p.opts.Scope, p.state)
	if p.store != nil {
		_ = p.store.SavePollState(context.Background(), p.opts.Scope, p.state)
	}
}

// ---------------------------------------------------------------------------
// State serialisation
// ---------------------------------------------------------------------------

func stateKey(scope string) string {
	return "triggers:" + scope
}

func loadState(st map[string]any, scope string) *pollState {
	key := stateKey(scope)
	raw, ok := st[key].(map[string]any)
	if !ok {
		return &pollState{}
	}
	ps := &pollState{}
	if v, ok := int64FromAny(raw["lastPoll"]); ok {
		ps.LastPoll = v
	}
	if arr, ok := raw["seenIDs"].([]any); ok {
		ps.SeenIDs = make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				ps.SeenIDs = append(ps.SeenIDs, s)
			}
		}
	}
	if s, ok := raw["cursor"].(string); ok {
		ps.Cursor = s
	}
	if v, ok := intFromAny(raw["idleCount"]); ok {
		ps.IdleCount = v
	}
	if v, ok := intFromAny(raw["pollCount"]); ok {
		ps.PollCount = v
	}
	return ps
}

func saveState(st map[string]any, scope string, ps *pollState) {
	seenAny := make([]any, len(ps.SeenIDs))
	for i, id := range ps.SeenIDs {
		seenAny[i] = id
	}
	st[stateKey(scope)] = map[string]any{
		"lastPoll":  float64(ps.LastPoll),
		"seenIDs":   seenAny,
		"cursor":    ps.Cursor,
		"idleCount": float64(ps.IdleCount),
		"pollCount": float64(ps.PollCount),
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// IntervalSeconds returns a time.Duration from seconds, clamped to the minimum.
func IntervalSeconds(secs int) time.Duration {
	if secs < 1 {
		secs = 60
	}
	d := time.Duration(secs) * time.Second
	if d < DefaultMinInterval {
		d = DefaultMinInterval
	}
	return d
}

func setFromSlice(ids []string) map[string]bool {
	s := make(map[string]bool, len(ids))
	for _, id := range ids {
		s[id] = true
	}
	return s
}

func hashScope(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum64())
}

func int64FromAny(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case int:
		return int64(t), true
	case string:
		// "1600000000" → int64
		var n int64
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

func intFromAny(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Debug / introspection
// ---------------------------------------------------------------------------

// Stats returns a snapshot of the poller's internal state for debugging.
func (p *Poller) Stats() map[string]any {
	return map[string]any{
		"scope":       p.opts.Scope,
		"interval":    p.opts.Interval.String(),
		"lastPoll":    time.Unix(p.state.LastPoll, 0).Format(time.RFC3339),
		"seenCount":   len(p.state.SeenIDs),
		"cursor":      p.state.Cursor,
		"idleCount":   p.state.IdleCount,
		"pollCount":   p.state.PollCount,
	}
}

// Reset clears the poller's state. Use with caution — previously-seen items
// will be re-emitted.
func (p *Poller) Reset(ctx *schema.ExecContext) {
	p.state = &pollState{}
	p.savePersistent(ctx)
}

// ---------------------------------------------------------------------------
// Compile-time guards
// ---------------------------------------------------------------------------

var _ = sort.Strings
var _ = strings.NewReader
var _ = sync.Mutex{}
