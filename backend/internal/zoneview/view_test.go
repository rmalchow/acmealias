package zoneview

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// fakeStore stands in for the database: a map the test swaps out, plus a switch
// that makes the next load fail the way an unreachable database does.
type fakeStore struct {
	mu      sync.Mutex
	aliases map[string][]store.ChallengeValue
	err     error
	loads   atomic.Int64
}

func (f *fakeStore) load(context.Context) (map[string][]store.ChallengeValue, error) {
	f.loads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string][]store.ChallengeValue, len(f.aliases))
	for k, v := range f.aliases {
		out[k] = append([]store.ChallengeValue{}, v...)
	}
	return out, nil
}

func (f *fakeStore) set(aliases map[string][]store.ChallengeValue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aliases, f.err = aliases, nil
}

func (f *fakeStore) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func live(values ...string) []store.ChallengeValue {
	out := make([]store.ChallengeValue, 0, len(values))
	for _, v := range values {
		out = append(out, store.ChallengeValue{Value: v, ExpiresAt: time.Now().Add(time.Hour)})
	}
	return out
}

func newTestView(t *testing.T, aliases map[string][]store.ChallengeValue) (*View, *fakeStore) {
	t.Helper()
	f := &fakeStore{aliases: aliases}
	v := New(f.load)
	if err := v.Refresh(context.Background()); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	return v, f
}

// The distinction the whole design turns on: an alias with no values exists and
// answers NOERROR-empty; a label that is not an alias does not exist at all.
func TestEmptySliceIsNotAbsentKey(t *testing.T) {
	v, _ := newTestView(t, map[string][]store.ChallengeValue{
		"shop-example-com": {},
		"blog-example-com": live("abc"),
	})

	vals, exists := v.Lookup("shop-example-com")
	if !exists {
		t.Fatal("alias with no values reported as non-existent: would answer NXDOMAIN after every cleanup")
	}
	if len(vals) != 0 {
		t.Fatalf("want no values, got %v", vals)
	}
	if !v.Has("shop-example-com") {
		t.Fatal("Has: alias with no values reported as non-existent")
	}

	if _, exists := v.Lookup("never-heard-of-it"); exists {
		t.Fatal("unknown label reported as existing: would answer NOERROR instead of NXDOMAIN")
	}
	if v.Has("never-heard-of-it") {
		t.Fatal("Has: unknown label reported as existing")
	}

	if vals, exists := v.Lookup("blog-example-com"); !exists || len(vals) != 1 || vals[0] != "abc" {
		t.Fatalf("want [abc] present, got %v exists=%v", vals, exists)
	}
}

// Expiry is applied at read time, not at refresh time: a value that expires
// between two refreshes stops being answered immediately, and its label stays.
func TestExpiryFilteredAtReadTime(t *testing.T) {
	now := time.Now()
	v, _ := newTestView(t, map[string][]store.ChallengeValue{
		"a": {
			{Value: "expired", ExpiresAt: now.Add(-time.Second)},
			{Value: "live", ExpiresAt: now.Add(time.Hour)},
		},
		"b": {{Value: "gone", ExpiresAt: now.Add(-time.Hour)}},
	})

	vals, exists := v.Lookup("a")
	if !exists || len(vals) != 1 || vals[0] != "live" {
		t.Fatalf("want only the unexpired value, got %v exists=%v", vals, exists)
	}

	// Wholly expired: still a known name, so NOERROR-empty, never NXDOMAIN.
	vals, exists = v.Lookup("b")
	if !exists {
		t.Fatal("label whose values all expired reported as non-existent")
	}
	if len(vals) != 0 {
		t.Fatalf("want no values, got %v", vals)
	}
}

// A value that expires while the snapshot sits untouched disappears on the very
// next read, with no refresh in between.
func TestValueStopsBeingAnsweredWhenItExpires(t *testing.T) {
	f := &fakeStore{aliases: map[string][]store.ChallengeValue{
		"a": {{Value: "soon", ExpiresAt: time.Now().Add(40 * time.Millisecond)}},
	}}
	v := New(f.load)
	if err := v.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if vals, _ := v.Lookup("a"); len(vals) != 1 {
		t.Fatalf("want the value answerable before expiry, got %v", vals)
	}
	time.Sleep(60 * time.Millisecond)
	loads := f.loads.Load()
	if vals, exists := v.Lookup("a"); !exists || len(vals) != 0 {
		t.Fatalf("want the label present with no values after expiry, got %v exists=%v", vals, exists)
	}
	if f.loads.Load() != loads {
		t.Fatal("Lookup hit the datastore; read-time expiry must need no refresh")
	}
}

// The write path: a synchronous Refresh after a write makes the value answerable
// by the time the response is written, without waiting for the timed refresh.
func TestWriteThenReadVisibility(t *testing.T) {
	v, f := newTestView(t, map[string][]store.ChallengeValue{"a": {}})

	f.set(map[string][]store.ChallengeValue{"a": live("published")})
	if err := v.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if vals, _ := v.Lookup("a"); len(vals) != 1 || vals[0] != "published" {
		t.Fatalf("want the published value answerable, got %v", vals)
	}

	// A cleanup leaves the label behind with no values, not an NXDOMAIN.
	f.set(map[string][]store.ChallengeValue{"a": {}})
	if err := v.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if vals, exists := v.Lookup("a"); !exists || len(vals) != 0 {
		t.Fatalf("after cleanup want present-and-empty, got %v exists=%v", vals, exists)
	}

	// A newly created alias must be answerable after the same synchronous refresh.
	f.set(map[string][]store.ChallengeValue{"a": {}, "brand-new": {}})
	if err := v.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !v.Has("brand-new") {
		t.Fatal("alias created then refreshed still answers NXDOMAIN")
	}
}

// Datastore unreachable: the last known snapshot keeps being served, and the
// failure is visible to the readiness probe and the metric.
func TestStaleSnapshotSurvivesStoreError(t *testing.T) {
	v, f := newTestView(t, map[string][]store.ChallengeValue{"a": live("known")})

	boom := errors.New("dial tcp: connection refused")
	f.fail(boom)
	if err := v.Refresh(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("want the store error surfaced, got %v", err)
	}

	if vals, exists := v.Lookup("a"); !exists || len(vals) != 1 || vals[0] != "known" {
		t.Fatalf("known value withdrawn while the store was down: %v exists=%v", vals, exists)
	}

	st := v.Status()
	if st.Reachable || !errors.Is(st.Err, boom) {
		t.Fatalf("want unreachable with the error recorded, got %+v", st)
	}
	if !st.Loaded || st.Labels != 1 {
		t.Fatalf("want the loaded snapshot still reported, got %+v", st)
	}

	f.set(map[string][]store.ChallengeValue{"a": live("known"), "b": {}})
	if err := v.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := v.Status(); !st.Reachable || st.Err != nil || st.Labels != 2 {
		t.Fatalf("want recovery, got %+v", st)
	}
}

// A view that has never loaded is not ready, and says so.
func TestNeverLoadedIsNotReady(t *testing.T) {
	f := &fakeStore{err: errors.New("down")}
	v := New(f.load)
	if err := v.Refresh(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	st := v.Status()
	if st.Loaded || st.Reachable {
		t.Fatalf("want not loaded and not reachable, got %+v", st)
	}
	if _, exists := v.Lookup("a"); exists {
		t.Fatal("an unloaded view must not claim any label exists")
	}
}

func TestRunRefreshesOnInterval(t *testing.T) {
	f := &fakeStore{aliases: map[string][]store.ChallengeValue{"a": {}}}
	v := New(f.load)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		v.Run(ctx, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	f.set(map[string][]store.ChallengeValue{"a": {}, "added-behind-our-back": {}})
	deadline := time.Now().Add(2 * time.Second)
	for !v.Has("added-behind-our-back") {
		if time.Now().After(deadline) {
			t.Fatal("timed refresh never picked up the change")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done
}

// -race: readers and refreshers hammering the same view.
func TestConcurrentReadersAndRefreshers(t *testing.T) {
	v, f := newTestView(t, map[string][]store.ChallengeValue{"a": live("v1")})

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				v.Lookup("a")
				v.Has("a")
				v.Status()
			}
		}()
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for ctx.Err() == nil {
				if n == 0 {
					f.set(map[string][]store.ChallengeValue{"a": live("v2")})
				} else {
					f.fail(errors.New("flap"))
				}
				_ = v.Refresh(ctx)
			}
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()
}

// Labels are matched case-insensitively and with or without the trailing dot,
// because that is how a query name arrives from the wire.
func TestLookupNormalizesTheQueriedLabel(t *testing.T) {
	v, _ := newTestView(t, map[string][]store.ChallengeValue{"a": live("x")})
	for _, label := range []string{"a", "A", "a.", "A."} {
		if vals, exists := v.Lookup(label); !exists || len(vals) != 1 {
			t.Fatalf("%q: want the value, got %v exists=%v", label, vals, exists)
		}
	}
}
