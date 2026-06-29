package observer

import (
	"context"

	"github.com/kite-io/kite/internal/event"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Watcher tails K8s Pod/Event/Node informers and forwards relevant signals to the agent mailbox.
type Watcher struct {
	mailbox chan<- event.Event
	client  client.Client
}

func newWatcher(mailbox chan<- event.Event, c client.Client) *Watcher {
	return &Watcher{mailbox: mailbox, client: c}
}

func (w *Watcher) start(ctx context.Context) {
	log.FromContext(ctx).Info("watcher started")
	// TODO: set up informers for corev1.Pod, corev1.Event, corev1.Node
	// TODO: on pod OOMKill         → w.send(event.Event{Kind: event.OOMKill, ...})
	// TODO: on pod crash/restart   → w.send(event.Event{Kind: event.PodCrash, ...})
	// TODO: on node condition flip → w.send(event.Event{Kind: event.DriftDetect, ...})
	<-ctx.Done()
}

// send is non-blocking: drops the event if the mailbox is full.
func (w *Watcher) send(e event.Event) {
	select {
	case w.mailbox <- e:
	default:
		// mailbox full — event dropped; next poll tick will cover drift
	}
}
