package queue

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionQueueSerializesSameSession(t *testing.T) {
	q := New()
	ctx := context.Background()

	var maxConcurrent int32
	var currentConcurrent int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := q.Enqueue(ctx, "web:owner", func(context.Context) (string, error) {
				nowConcurrent := atomic.AddInt32(&currentConcurrent, 1)
				defer atomic.AddInt32(&currentConcurrent, -1)

				for {
					currentMax := atomic.LoadInt32(&maxConcurrent)
					if nowConcurrent <= currentMax || atomic.CompareAndSwapInt32(&maxConcurrent, currentMax, nowConcurrent) {
						break
					}
				}

				time.Sleep(5 * time.Millisecond)
				return "ok", nil
			})
			if err != nil {
				t.Errorf("Enqueue() error = %v", err)
			}
		}()
	}

	wg.Wait()

	if got := atomic.LoadInt32(&maxConcurrent); got != 1 {
		t.Fatalf("expected max concurrency 1 for same session key, got %d", got)
	}
}
