package scheduler

import (
	"testing"
	"time"
)

func TestDueInterval(t *testing.T) {
	last := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := map[string]any{"mode": "interval", "amount": 5, "unit": "minutes"}
	if due(p, last, last.Add(4*time.Minute)) {
		t.Fatal("should not fire before the interval elapses")
	}
	if !due(p, last, last.Add(5*time.Minute)) {
		t.Fatal("should fire once the interval elapses")
	}
}

func TestDueCron(t *testing.T) {
	last := time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC) // last fired at 00:00:30
	p := map[string]any{"mode": "cron", "cron": "* * * * *"} // every minute
	if due(p, last, time.Date(2026, 1, 1, 0, 0, 45, 0, time.UTC)) {
		t.Fatal("next run is 00:01:00 — not due at 00:00:45")
	}
	if !due(p, last, time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)) {
		t.Fatal("should be due at 00:01:00")
	}
}

func TestDueCronInvalid(t *testing.T) {
	if due(map[string]any{"mode": "cron", "cron": "not a cron"}, time.Now().Add(-time.Hour), time.Now()) {
		t.Fatal("invalid cron must never be due")
	}
}
