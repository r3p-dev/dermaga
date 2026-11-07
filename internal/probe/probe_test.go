package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func lines(t *testing.T, b *bytes.Buffer) []Attempt {
	t.Helper()
	var out []Attempt
	for _, l := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		var a Attempt
		if err := json.Unmarshal([]byte(l), &a); err != nil {
			t.Fatalf("not a JSON line: %q", l)
		}
		out = append(out, a)
	}
	return out
}

func TestHealthyAfterConsecutiveSuccessesResettingOnFailure(t *testing.T) {
	codes := []int{200, 503, 200, 302, 200} // the 503 resets the count
	var i atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := codes[min(int(i.Add(1))-1, len(codes)-1)]
		if c == 302 {
			w.Header().Set("Location", "/elsewhere") // not followed
		}
		w.WriteHeader(c)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := Run(context.Background(), Options{URL: srv.URL, Successes: 3, Interval: 5 * time.Millisecond, Timeout: 5 * time.Second}, &out)
	if err != nil {
		t.Fatal(err)
	}
	got := lines(t, &out)
	if len(got) != 5 || got[1].OK || got[1].Status != 503 || got[1].N != 0 || got[4].N != 3 || got[3].Status != 302 {
		t.Fatalf("attempts: %+v", got)
	}
}

func TestUnhealthyAtDeadline(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	var out bytes.Buffer
	start := time.Now()
	err := Run(context.Background(), Options{URL: srv.URL, Successes: 3, Interval: 20 * time.Millisecond, Timeout: 150 * time.Millisecond}, &out)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
	for _, a := range lines(t, &out) {
		if a.OK || a.Error == "" {
			t.Fatalf("attempt %+v", a)
		}
	}
}

func TestConnectionRefusedIsAFailedAttempt(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	var out bytes.Buffer
	if err := Run(context.Background(), Options{URL: url, Successes: 1, Interval: 10 * time.Millisecond, Timeout: 50 * time.Millisecond}, &out); err == nil {
		t.Fatal("closed port reported healthy")
	}
	if a := lines(t, &out)[0]; a.OK || !strings.Contains(a.Error, "refused") {
		t.Fatalf("attempt %+v", a)
	}
}
