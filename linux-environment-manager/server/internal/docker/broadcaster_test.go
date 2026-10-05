package docker

import (
	"sync"
	"testing"
	"time"
)

// TestBroadcasterUnsubscribeDuringBroadcast reproduces the race where a
// subscriber disconnects while Broadcast is fanning out. Before the fix,
// Unsubscribe closed the channel and a concurrent send panicked with "send on
// closed channel". Run with -race.
func TestBroadcasterUnsubscribeDuringBroadcast(t *testing.T) {
	b := NewBroadcaster()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				b.Broadcast("dev-1", DockerState{Available: true})
			}
		}
	}()

	for i := 0; i < 300; i++ {
		ch := b.Subscribe("dev-1")
		select {
		case <-ch:
		case <-time.After(time.Millisecond):
		}
		b.Unsubscribe("dev-1", ch)
	}

	close(stop)
	wg.Wait()

	if got := b.SubscriberCount("dev-1"); got != 0 {
		t.Fatalf("expected no subscribers after cleanup, got %d", got)
	}
}
