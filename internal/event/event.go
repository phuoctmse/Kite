package event

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"
)

type Kind string

const (
	PodCrash    Kind = "pod_crash"
	OOMKill     Kind = "oom_kill"
	PollTick    Kind = "poll_tick"
	DriftDetect Kind = "drift_detect"
	UserQuery   Kind = "user_query"
)

type Event struct {
	Kind      Kind
	Namespace string
	Resource  string
	RawObj    runtime.Object // nil for PollTick
	Timestamp time.Time
}
