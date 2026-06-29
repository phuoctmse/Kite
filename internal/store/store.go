package store

import (
	"time"

	"github.com/kite-io/kite/internal/brain"
)

// Store persists events and action results for audit and brain context injection.
// Write failures must never block the agent event loop.
type Store interface {
	WriteEvent(kind, namespace, resource string, ts time.Time) error
	WriteAction(result ActionResult) error
	WriteHistory(turn brain.Turn) error
	RecentHistory(n int) ([]brain.Turn, error)
	RecentSimilarEvents(kind, namespace string, n int) ([]StoredEvent, error)
	Close() error
}

type ActionResult struct {
	ActionName string
	Output     string
	Err        string // empty when no error
	Timestamp  time.Time
}

type StoredEvent struct {
	Kind      string
	Namespace string
	Resource  string
	Timestamp time.Time
}
