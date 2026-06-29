package observer

import (
	"context"
	"time"

	"github.com/kite-io/kite/internal/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Poller fires a periodic EventPollTick to trigger drift detection.
type Poller struct {
	interval time.Duration
	mailbox  chan<- event.Event
}

func newPoller(interval time.Duration, mailbox chan<- event.Event) *Poller {
	return &Poller{interval: interval, mailbox: mailbox}
}

func (p *Poller) start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	log.FromContext(ctx).Info("poller started", "interval", p.interval)

	for {
		select {
		case t := <-ticker.C:
			select {
			case p.mailbox <- event.Event{Kind: event.PollTick, Timestamp: t}:
			default:
				// mailbox full — skip this tick
			}
		case <-ctx.Done():
			return
		}
	}
}
