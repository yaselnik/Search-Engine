package frontier

type QueuedPage struct {
	URL      string
	priority float64
}

// PageQueue implements heap.Interface for *QueuedPage.
// It acts as a Max-Heap sorted by QueuedPage.priority.
// This ensures that higher-value pages (e.g., higher PageRank)
// are fetched first within a specific host.
type PageQueue []*QueuedPage

func (pq PageQueue) Len() int { return len(pq) }

func (pq PageQueue) Less(i, j int) bool {
	return pq[i].priority > pq[j].priority
}

func (pq PageQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PageQueue) Push(x any) {
	*pq = append(*pq, x.(*QueuedPage))
}

func (pq *PageQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	return item
}
