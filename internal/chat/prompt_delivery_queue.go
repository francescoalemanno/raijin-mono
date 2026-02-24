package chat

type promptDeliveryQueue struct {
	items []queuedPrompt
}

func newPromptDeliveryQueue() promptDeliveryQueue {
	return promptDeliveryQueue{}
}

func (q *promptDeliveryQueue) Enqueue(prompt queuedPrompt) {
	q.items = append(q.items, prompt)
}

func (q *promptDeliveryQueue) Dequeue() (queuedPrompt, bool) {
	if len(q.items) == 0 {
		return queuedPrompt{}, false
	}
	next := q.items[0]
	q.items = q.items[1:]
	return next, true
}

func (q *promptDeliveryQueue) Len() int {
	return len(q.items)
}

func (q *promptDeliveryQueue) Clear() {
	q.items = nil
}
