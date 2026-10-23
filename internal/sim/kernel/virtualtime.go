package kernel

import (
	"container/heap"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// scheduledEvent is a node completion (or failure) due at a virtual time.
type scheduledEvent struct {
	time   time.Time
	nodeID engine.NodeID
	// mutation carries the output of a successful node; it may be nil.
	mutation *engine.StateMutation
	// err is non-nil when the node failed.
	err error
}

// virtualTimeHeap is a min-heap of scheduledEvents ordered by (time, nodeID).
type virtualTimeHeap []scheduledEvent

func (h virtualTimeHeap) Len() int { return len(h) }

func (h virtualTimeHeap) Less(i, j int) bool {
	if h[i].time.Equal(h[j].time) {
		return h[i].nodeID < h[j].nodeID
	}
	return h[i].time.Before(h[j].time)
}

func (h virtualTimeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *virtualTimeHeap) Push(x any) { *h = append(*h, x.(scheduledEvent)) }

func (h *virtualTimeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// push adds ev to the heap.
func (h *virtualTimeHeap) push(ev scheduledEvent) { heap.Push(h, ev) }

// pop removes and returns the earliest event (smallest node ID on ties).
func (h *virtualTimeHeap) pop() scheduledEvent { return heap.Pop(h).(scheduledEvent) }

// popInstant removes and returns every event due at the earliest virtual
// time, in node ID order. The heap must not be empty.
func (h *virtualTimeHeap) popInstant() []scheduledEvent {
	first := h.pop()
	batch := []scheduledEvent{first}
	for h.Len() > 0 && (*h)[0].time.Equal(first.time) {
		batch = append(batch, h.pop())
	}
	return batch
}
