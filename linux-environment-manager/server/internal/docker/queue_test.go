package docker

import (
	"testing"
	"time"
)

func TestQueueScopesResultsToDevice(t *testing.T) {
	q := NewQueue(time.Minute)
	id := q.EnqueueForDevice("device-a", "list", "", "")

	q.StoreResult(&Response{DeviceID: "device-b", RequestID: id, Status: "completed"})
	if got := q.GetAndConsumeResult("device-a", id); got != nil {
		t.Fatalf("result from another device was accepted: %#v", got)
	}

	q.StoreResult(&Response{DeviceID: "device-a", RequestID: id, Status: "completed"})
	if got := q.GetAndConsumeResult("device-b", id); got != nil {
		t.Fatalf("result was visible to another device: %#v", got)
	}
	if got := q.GetAndConsumeResult("device-a", id); got == nil || got.Status != "completed" {
		t.Fatalf("expected result for owning device, got %#v", got)
	}
}
