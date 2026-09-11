package main

import (
	"sync/atomic"
	"testing"
)

func TestDeferredMainActivationDeliversPendingRequest(t *testing.T) {
	activation := &deferredMainActivation{}
	activation.request()

	var calls atomic.Int32
	activation.bind(func() { calls.Add(1) })
	if got := calls.Load(); got != 1 {
		t.Fatalf("show calls = %d, want 1", got)
	}
}

func TestDeferredMainActivationHandlesLaterRequests(t *testing.T) {
	activation := &deferredMainActivation{}
	var calls atomic.Int32
	activation.bind(func() { calls.Add(1) })

	activation.request()
	activation.request()
	if got := calls.Load(); got != 2 {
		t.Fatalf("show calls = %d, want 2", got)
	}
}
