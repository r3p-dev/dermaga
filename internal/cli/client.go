package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/name"
)

// call sends one JSON request to the daemon over its Unix socket.
func call(method, path string, in, out any) error {
	return callTimeout(method, path, 60*time.Second, in, out)
}

// callTimeout sends one JSON request to the daemon over its Unix socket with a custom timeout.
func callTimeout(method, path string, timeout time.Duration, in, out any) error {
	p, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", p.Socket())
		},
	}}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://"+name.Name+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("daemon not reachable at %s (systemctl --user status %s): %w", p.Socket(), name.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
