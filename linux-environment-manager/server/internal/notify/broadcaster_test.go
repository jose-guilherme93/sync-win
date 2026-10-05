package notify

import (
	"sync"
	"testing"
	"time"
)

// TestNotifBroadcasterUnsubscribeDuringBroadcast reproduces the race where a
// subscriber disconnects while Broadcast is fanning out. Before the fix,
// Unsubscribe closed the channel and a concurrent send panicked with "send on
// closed channel" (fatal in the status-watcher goroutine). Run with -race.
func TestNotifBroadcasterUnsubscribeDuringBroadcast(t *testing.T) {
	b := NewNotifBroadcaster()
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
				b.Broadcast("owner-1", NotifEvent{Type: "test"})
			}
		}
	}()

	for i := 0; i < 300; i++ {
		ch := b.Subscribe("owner-1")
		select {
		case <-ch:
		case <-time.After(time.Millisecond):
		}
		b.Unsubscribe("owner-1", ch)
	}

	close(stop)
	wg.Wait()
}
