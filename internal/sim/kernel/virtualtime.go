package kernel

import (
	"container/heap"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// scheduledEvent represents a node execution completion at a specific virtual time.
type scheduledEvent struct {
	time    time.Time
	nodeID  engine.NodeID
	outcome sim.BehaviorOutcome
}

// virtualTimeHeap implements heap.Interface and holds scheduledEvents.
// It acts as a min-heap ordered by (time, nodeID).
type virtualTimeHeap []scheduledEvent

func (h virtualTimeHeap) Len() int { return len(h) }

func (h virtualTimeHeap) Less(i, j int) bool {
	if h[i].time.Equal(h[j].time) {
		return string(h[i].nodeID) < string(h[j].nodeID)
	}
	return h[i].time.Before(h[j].time)
}

func (h virtualTimeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *virtualTimeHeap) Push(x any) {
	*h = append(*h, x.(scheduledEvent))
}

func (h *virtualTimeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// push adds a new scheduled event to the heap.
func (h *virtualTimeHeap) push(ev scheduledEvent) {
	heap.Push(h, ev)
}

// pop removes and returns the earliest scheduled event from the heap.
func (h *virtualTimeHeap) pop() scheduledEvent {
	return heap.Pop(h).(scheduledEvent)
}
