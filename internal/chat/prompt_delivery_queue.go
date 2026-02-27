package chat

import "strings"

type promptDeliveryQueue struct {
	items []queuedPrompt
}

func newPromptDeliveryQueue() promptDeliveryQueue {
	return promptDeliveryQueue{}
}

func (q *promptDeliveryQueue) Enqueue(prompt queuedPrompt) {
	q.items = append(q.items, prompt)
}

func (q *promptDeliveryQueue) DequeueAll() (queuedPrompt, bool) {
	if len(q.items) == 0 {
		return queuedPrompt{}, false
	}
	// Combine all prompts into one, joining inputs with "\n".
	// Use options from the first prompt.
	combined := q.items[0]
	var inputs []string
	for _, item := range q.items {
		inputs = append(inputs, item.Input)
	}
	combined.Input = strings.Join(inputs, "\n")
	q.items = nil
	return combined, true
}

func (q *promptDeliveryQueue) Len() int {
	return len(q.items)
}

func (q *promptDeliveryQueue) Clear() {
	q.items = nil
}
