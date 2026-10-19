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

// The result handler needs to know whether a completed result is a container
// list before it can refresh the cached snapshot. The type must therefore
// survive from enqueue until the result is consumed.
func TestQueueRemembersRequestType(t *testing.T) {
	q := NewQueue(time.Minute)
	id := q.EnqueueForDevice("device-a", "list", "", "")

	if got := q.RequestType(id); got != "list" {
		t.Fatalf("type before result = %q, want list", got)
	}

	q.StoreResult(&Response{DeviceID: "device-a", RequestID: id, Status: "completed"})
	if got := q.RequestType(id); got != "list" {
		t.Fatalf("type after result but before consumption = %q, want list", got)
	}

	q.GetAndConsumeResult("device-a", id)
	if got := q.RequestType(id); got != "" {
		t.Fatalf("type after consumption = %q, want empty", got)
	}
}
