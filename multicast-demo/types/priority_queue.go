package types

import "container/heap"

type MessagePQ []*Message

func (pq MessagePQ) Len() int { return len(pq) }

func (pq MessagePQ) Less(i, j int) bool {
	if (pq[i].Priority < pq[j].Priority) || (pq[i].Priority == pq[j].Priority && pq[i].Sender < pq[j].Sender) {
		return true
	} else {
		return false
	}
}

func (pq MessagePQ) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *MessagePQ) Push(x any) {
	n := len(*pq)
	msg := x.(*Message)
	msg.Index = n
	*pq = append(*pq, msg)
}

func (pq *MessagePQ) Pop() any {
	old := *pq
	n := len(old)
	msg := old[n-1]
	old[n-1] = nil
	msg.Index = -1
	*pq = old[0 : n-1]

	return msg
}

func (pq *MessagePQ) Update(message *Message, priority int) {
	message.Priority = priority
	heap.Fix(pq, message.Index)
}

func (pq *MessagePQ) Remove(message *Message) {
	heap.Remove(pq, message.Index)
}

func (pq MessagePQ) FindById(Id string) *Message {
	for _, item := range pq {
		if item.Id == Id {
			return item
		}
	}
	return nil
}
