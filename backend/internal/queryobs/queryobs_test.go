package queryobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestObserveCountsPerLabelAndStampsFirstAndLast(t *testing.T) {
	c := NewCounter()
	c.Observe("demo-example-com")
	time.Sleep(2 * time.Millisecond)
	c.Observe("demo-example-com")
	c.Observe("other")

	obs, _ := c.Drain()
	if got := obs["demo-example-com"].Count; got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
	if got := obs["other"].Count; got != 1 {
		t.Errorf("other count = %d, want 1", got)
	}

	// The timestamps must be the queries' own, which is the whole reason they are
	// carried rather than being filled in with the flush window's bounds.
	d := obs["demo-example-com"]
	if !d.Last.After(d.First) {
		t.Errorf("last %v is not after first %v", d.Last, d.First)
	}
}

func TestDrainResetsCountsAndWindow(t *testing.T) {
	c := NewCounter()
	c.Observe("a")

	first, since := c.Drain()
	if len(first) != 1 {
		t.Fatalf("first drain returned %d labels, want 1", len(first))
	}
	if since.IsZero() {
		t.Error("window start is zero")
	}

	second, nextSince := c.Drain()
	if second != nil {
		t.Errorf("second drain returned %v, want nil", second)
	}
	if !nextSince.After(since) && !nextSince.Equal(since) {
		t.Errorf("window did not advance: %v then %v", since, nextSince)
	}
}

func TestNilCounterIsANoOp(t *testing.T) {
	// The DNS handler holds a possibly-nil counter and must not have to guard it.
	var c *Counter
	c.Observe("a")
	if obs, _ := c.Drain(); obs != nil {
		t.Errorf("nil counter drained %v", obs)
	}
}

func TestFlushCarriesTheWindowItAccumulatedOver(t *testing.T) {
	c := NewCounter()
	start := time.Now()
	c.Observe("a")

	var gotFrom, gotTo time.Time
	c.flushOnce(context.Background(), func(_ context.Context, _ map[string]Observation, from, to time.Time) error {
		gotFrom, gotTo = from, to
		return nil
	}, discard())

	if gotFrom.After(start) {
		t.Errorf("window start %v is after the first observation %v", gotFrom, start)
	}
	if gotTo.Before(gotFrom) {
		t.Errorf("window end %v precedes its start %v", gotTo, gotFrom)
	}
}

func TestFailedFlushIsRetriedWithItsOriginalWindow(t *testing.T) {
	// A datastore outage must delay observations, never drop them — and the retry
	// has to cover the period the queries actually happened in, not the one that
	// began when the flush failed.
	c := NewCounter()
	c.Observe("a")
	c.Observe("a")
	firstWindowStart := c.since

	c.flushOnce(context.Background(), func(_ context.Context, _ map[string]Observation, _, _ time.Time) error {
		return errors.New("datastore down")
	}, discard())

	c.Observe("a") // accumulated while the first batch was in limbo

	var got map[string]Observation
	var from time.Time
	c.flushOnce(context.Background(), func(_ context.Context, o map[string]Observation, f, _ time.Time) error {
		got, from = o, f
		return nil
	}, discard())

	if got["a"].Count != 3 {
		t.Errorf("count after retry = %d, want 3 (2 returned + 1 new)", got["a"].Count)
	}
	if from.After(firstWindowStart) {
		t.Errorf("retry window starts at %v, after the original %v — the returned queries would be misattributed",
			from, firstWindowStart)
	}
}

func TestRunFlushesOnceMoreOnShutdown(t *testing.T) {
	c := NewCounter()
	ctx, cancel := context.WithCancel(context.Background())

	var mu sync.Mutex
	flushed := 0
	done := make(chan struct{})
	go func() {
		c.Run(ctx, time.Hour, func(_ context.Context, _ map[string]Observation, _, _ time.Time) error {
			mu.Lock()
			flushed++
			mu.Unlock()
			return nil
		}, discard())
		close(done)
	}()

	c.Observe("a")
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if flushed != 1 {
		t.Errorf("flushed %d times on shutdown, want 1 — the final window was dropped", flushed)
	}
}

func TestObserveIsSafeConcurrently(t *testing.T) {
	c := NewCounter()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				c.Observe("a")
			}
		}()
	}
	wg.Wait()

	obs, _ := c.Drain()
	if obs["a"].Count != 1000 {
		t.Errorf("count = %d, want 1000", obs["a"].Count)
	}
}
