package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSuppressDuplicateScansConcurrent(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	forward := func(qr string) (int, string) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return 200, `{"ok":true,"opened":true}`
	}
	scan := suppressDuplicateScans(time.Second, forward)

	var wg sync.WaitGroup
	type result struct {
		status int
		body   string
	}
	results := make(chan result, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, body := scan("same-qr")
			results <- result{status: status, body: body}
		}()
	}
	<-started
	time.Sleep(10 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)
	if got := calls.Load(); got != 1 {
		t.Fatalf("forward called %d times, want 1", got)
	}
	for result := range results {
		if result.status != 200 || result.body != `{"ok":true,"opened":true}` {
			t.Fatalf("unexpected duplicate result %#v", result)
		}
	}
}

func TestSuppressDuplicateScansPerCredentialAndWindow(t *testing.T) {
	var calls atomic.Int32
	scan := suppressDuplicateScans(20*time.Millisecond, func(qr string) (int, string) {
		calls.Add(1)
		return 200, qr
	})

	_, _ = scan("member-a")
	_, _ = scan("member-a")
	_, _ = scan("member-b")
	if got := calls.Load(); got != 2 {
		t.Fatalf("forward called %d times before expiry, want 2", got)
	}
	time.Sleep(25 * time.Millisecond)
	_, _ = scan("member-a")
	if got := calls.Load(); got != 3 {
		t.Fatalf("forward called %d times after expiry, want 3", got)
	}
}
