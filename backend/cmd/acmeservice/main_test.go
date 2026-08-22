package main

import (
	"bytes"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// serveOne starts an HTTP server on a loopback port whose handler blocks for
// hold, and returns the server plus its address.
func serveOne(t *testing.T, hold time.Duration, entered chan<- struct{}) (*http.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		time.Sleep(hold)
		w.Write([]byte("done"))
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return srv, "http://" + ln.Addr().String() + "/"
}

func logTo(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// A request in flight when the signal arrives finishes before the process
// exits — the whole point of draining rather than closing.
func TestShutdownLetsInFlightRequestFinish(t *testing.T) {
	entered := make(chan struct{})
	srv, url := serveOne(t, 300*time.Millisecond, entered)

	type result struct {
		body string
		err  error
	}
	res := make(chan result, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			res <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b := make([]byte, 4)
		resp.Body.Read(b)
		res <- result{body: string(b)}
	}()

	<-entered // the handler is running: now signal.
	var buf bytes.Buffer
	shutdown(logTo(&buf), 5*time.Second, stopper{name: "http", drain: srv.Shutdown, force: func() { srv.Close() }})

	r := <-res
	if r.err != nil {
		t.Fatalf("in-flight request failed across shutdown: %v", r.err)
	}
	if r.body != "done" {
		t.Errorf("in-flight response %q, want %q", r.body, "done")
	}
	if strings.Contains(buf.String(), "shutdown forced") {
		t.Errorf("shutdown was forced although the request finished within the grace period: %s", buf.String())
	}
}

// Work still running when the grace period expires is cut off, and says so.
func TestShutdownForcesAfterGrace(t *testing.T) {
	entered := make(chan struct{})
	srv, url := serveOne(t, 2*time.Second, entered)

	go http.Get(url) //nolint:errcheck // the point is that this one is cut off
	<-entered

	var buf bytes.Buffer
	forced := make(chan struct{}, 1)
	start := time.Now()
	shutdown(logTo(&buf), 100*time.Millisecond, stopper{
		name:  "http",
		drain: srv.Shutdown,
		force: func() { srv.Close(); forced <- struct{}{} },
	})

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("shutdown waited %s, want a forced exit at the 100ms grace period", elapsed)
	}
	if len(forced) != 1 {
		t.Error("the forced close was not called after the grace period expired")
	}
	if !strings.Contains(buf.String(), "shutdown forced") {
		t.Errorf("forced shutdown was not logged: %s", buf.String())
	}
}

// stoppers wires the real components; check it produces one entry per server
// plus DNS, since a listener missing from that list would never be drained.
func TestStoppersCoversEveryServer(t *testing.T) {
	got := stoppers(nil, []*http.Server{{}, {}, {}})
	if len(got) != 4 {
		t.Fatalf("stoppers: %d entries, want 4 (dns + three HTTP listeners)", len(got))
	}
	if got[0].name != "dns" {
		t.Errorf("first stopper is %q, want dns", got[0].name)
	}
	// Each HTTP entry must close its own server, not the last one in the loop.
	for i, s := range got[1:] {
		if s.drain == nil || s.force == nil {
			t.Errorf("http stopper %d is incomplete", i)
		}
	}
}
