package frontier

import "time"

type HostState struct {
	name       string
	lastReq    time.Time
	crawlDelay time.Duration
	readyAt    time.Time
	inQueue    bool
}

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
