package observer

import (
	"context"
	"sync"
	"time"

	"github.com/kite-io/kite/internal/event"
	corev1 "k8s.io/api/core/v1"
	toolscache "k8s.io/client-go/tools/cache"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Watcher tails K8s Pod/Event/Node informers and forwards relevant signals to the agent mailbox.
type Watcher struct {
	mailbox chan<- event.Event
	cache   ctrlcache.Cache
}

func newWatcher(mailbox chan<- event.Event, c ctrlcache.Cache) *Watcher {
	return &Watcher{mailbox: mailbox, cache: c}
}

func (w *Watcher) start(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("watcher started")

	// Get informers from cache
	podInformer, err := w.cache.GetInformer(ctx, &corev1.Pod{})
	if err != nil {
		logger.Error(err, "failed to get pod informer")
		return
	}

	eventInformer, err := w.cache.GetInformer(ctx, &corev1.Event{})
	if err != nil {
		logger.Error(err, "failed to get event informer")
		return
	}

	nodeInformer, err := w.cache.GetInformer(ctx, &corev1.Node{})
	if err != nil {
		logger.Error(err, "failed to get node informer")
		return
	}

	// Track state for detecting changes (used to suppress duplicates)
	podStates := &sync.Map{}  // key: namespace/name → *corev1.Pod
	nodeStates := &sync.Map{} // key: name → *corev1.Node
	_ = podStates
	_ = nodeStates

	// Add event handlers for pods
	if _, err = podInformer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldPod, ok1 := oldObj.(*corev1.Pod)
			newPod, ok2 := newObj.(*corev1.Pod)
			if !ok1 || !ok2 {
				return
			}
			w.handlePodUpdate(oldPod, newPod)
		},
	}); err != nil {
		logger.Error(err, "failed to add pod event handler")
		return
	}

	// Add event handlers for K8s events (Warning events → DriftDetect)
	if _, err = eventInformer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ev, ok := obj.(*corev1.Event)
			if !ok {
				return
			}
			if ev.Type == corev1.EventTypeWarning {
				w.send(event.Event{
					Kind:      event.DriftDetect,
					Namespace: ev.InvolvedObject.Namespace,
					Resource:  ev.InvolvedObject.Name,
					RawObj:    ev,
					Timestamp: time.Now(),
				})
			}
		},
	}); err != nil {
		logger.Error(err, "failed to add event handler")
		return
	}

	// Add event handlers for nodes
	if _, err = nodeInformer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldNode, ok1 := oldObj.(*corev1.Node)
			newNode, ok2 := newObj.(*corev1.Node)
			if !ok1 || !ok2 {
				return
			}
			w.handleNodeUpdate(oldNode, newNode)
		},
	}); err != nil {
		logger.Error(err, "failed to add node event handler")
		return
	}

	logger.Info("watcher event handlers registered")
	<-ctx.Done()
	logger.Info("watcher stopped")
}

func (w *Watcher) handlePodUpdate(oldPod, newPod *corev1.Pod) {
	for i, cs := range newPod.Status.ContainerStatuses {
		// Check for OOM kills
		if cs.LastTerminationState.Terminated != nil &&
			cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
			if i < len(oldPod.Status.ContainerStatuses) {
				oldCS := oldPod.Status.ContainerStatuses[i]
				if oldCS.LastTerminationState.Terminated == nil ||
					!oldCS.LastTerminationState.Terminated.FinishedAt.Equal(
						&cs.LastTerminationState.Terminated.FinishedAt) {
					w.send(event.Event{
						Kind:      event.OOMKill,
						Namespace: newPod.Namespace,
						Resource:  newPod.Name,
						RawObj:    newPod,
						Timestamp: time.Now(),
					})
				}
			}
		}

		// Check for crash loops (restart count increased)
		if i < len(oldPod.Status.ContainerStatuses) {
			oldCS := oldPod.Status.ContainerStatuses[i]
			if cs.RestartCount > oldCS.RestartCount {
				w.send(event.Event{
					Kind:      event.PodCrash,
					Namespace: newPod.Namespace,
					Resource:  newPod.Name,
					RawObj:    newPod,
					Timestamp: time.Now(),
				})
			}
		}
	}
}

func (w *Watcher) handleNodeUpdate(oldNode, newNode *corev1.Node) {
	oldConditions := make(map[corev1.NodeConditionType]corev1.ConditionStatus, len(oldNode.Status.Conditions))
	for _, cond := range oldNode.Status.Conditions {
		oldConditions[cond.Type] = cond.Status
	}

	for _, cond := range newNode.Status.Conditions {
		if oldStatus, ok := oldConditions[cond.Type]; ok && oldStatus != cond.Status {
			w.send(event.Event{
				Kind:      event.DriftDetect,
				Namespace: "", // nodes are cluster-scoped
				Resource:  newNode.Name,
				RawObj:    newNode,
				Timestamp: time.Now(),
			})
			break
		}
	}
}

// send is non-blocking: drops the event if the mailbox is full.
func (w *Watcher) send(e event.Event) {
	select {
	case w.mailbox <- e:
	default:
		// mailbox full — event dropped; next poll tick will cover drift
	}
}
