// Package probe implements `dermaga probe` (ARCHITECTURE D2): run inside the
// rootless network namespace (podman unshare --rootless-netns dermaga probe …),
// it requires K consecutive HTTP successes before a deadline and reports each
// attempt as a JSON line. Ported from the M0 spike (spikes/m0/probe at eb9ad64).
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type Options struct {
	URL       string
	DNS       string // resolver for container names (aardvark-dns at the network gateway), e.g. 10.89.0.1:53
	Successes int
	Interval  time.Duration
	Timeout   time.Duration
}

// Attempt is one JSON line on the output.
type Attempt struct {
	T      float64 `json:"t"` // seconds since start
	OK     bool    `json:"ok"`
	Status int     `json:"status,omitempty"`
	N      int     `json:"n"` // consecutive successes so far
	Error  string  `json:"error,omitempty"`
}

// Run returns nil once o.Successes consecutive 2xx/3xx responses were seen.
func Run(ctx context.Context, o Options, out io.Writer) error {
	if o.URL == "" || o.Successes < 1 || o.Interval <= 0 || o.Timeout <= 0 {
		return errors.New("probe: url, successes >= 1, interval and timeout are required")
	}
	d := &net.Dialer{Timeout: 2 * time.Second}
	if o.DNS != "" {
		d.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, "udp", o.DNS)
		}}
	}
	c := &http.Client{
		Timeout:       3 * time.Second,
		Transport:     &http.Transport{DialContext: d.DialContext, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	enc := json.NewEncoder(out)
	start, n := time.Now(), 0
	for {
		a := Attempt{T: float64(time.Since(start).Milliseconds()) / 1000}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.URL, nil)
		if err != nil {
			return err
		}
		resp, err := c.Do(req)
		if err == nil {
			resp.Body.Close()
			a.Status = resp.StatusCode
			if resp.StatusCode >= 400 {
				err = fmt.Errorf("status %d", resp.StatusCode)
			}
		}
		if err == nil {
			n++
			a.OK = true
		} else {
			n = 0
			a.Error = err.Error()
		}
		a.N = n
		enc.Encode(a)
		if n == o.Successes {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("unhealthy: no %d consecutive successes within %s", o.Successes, o.Timeout)
		case <-time.After(o.Interval):
		}
	}
}
