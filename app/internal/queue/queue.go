package queue

import (
	"context"
	"sync"
)

type keySemaphore struct {
	channel chan struct{}
}

type SessionQueue struct {
	mu         sync.Mutex
	semaphores map[string]*keySemaphore
}

func New() *SessionQueue {
	return &SessionQueue{
		semaphores: make(map[string]*keySemaphore),
	}
}

func (q *SessionQueue) Enqueue(
	ctx context.Context,
	sessionKey string,
	task func(context.Context) (string, error),
) (string, error) {
	sem := q.semaphoreForKey(sessionKey)

	select {
	case sem.channel <- struct{}{}:
		defer func() { <-sem.channel }()
		return task(ctx)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (q *SessionQueue) semaphoreForKey(sessionKey string) *keySemaphore {
	q.mu.Lock()
	defer q.mu.Unlock()

	if sem, ok := q.semaphores[sessionKey]; ok {
		return sem
	}

	sem := &keySemaphore{channel: make(chan struct{}, 1)}
	q.semaphores[sessionKey] = sem
	return sem
}
