package frontier

import "time"

// HostState tracks politeness constraints and scheduling state for a single host.
type HostState struct {
	name       string
	lastReq    time.Time
	crawlDelay time.Duration
	readyAt    time.Time
	inQueue    bool // true when the host is currently present in HostsQueue
	inFlight   bool // true while a worker holds an active lease for this host
}

// HostsQueue implements heap.Interface as a min-heap ordered by HostState.readyAt.
// The host whose politeness timer expires earliest is always at the root of the heap.
type HostsQueue []*HostState

func (pq HostsQueue) Len() int { return len(pq) }

func (pq HostsQueue) Less(i, j int) bool {
	return pq[i].readyAt.Before(pq[j].readyAt)
}

func (pq HostsQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *HostsQueue) Push(x any) {
	*pq = append(*pq, x.(*HostState))
}

func (pq *HostsQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	return item
}
