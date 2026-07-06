package observer

import (
	"context"
	"time"

	"github.com/kite-io/kite/internal/event"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
)

// Observer bundles the Watcher and Poller into a single startable unit.
type Observer struct {
	watcher *Watcher
	poller  *Poller
}

func New(mailbox chan<- event.Event, c ctrlcache.Cache, pollInterval time.Duration) *Observer {
	return &Observer{
		watcher: newWatcher(mailbox, c),
		poller:  newPoller(pollInterval, mailbox),
	}
}

// Start launches both sub-components as goroutines and returns immediately.
func (o *Observer) Start(ctx context.Context) {
	go o.watcher.start(ctx)
	go o.poller.start(ctx)
}
