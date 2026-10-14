package frontier

type QueuedPage struct {
	URL      string
	priority float64
}

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
