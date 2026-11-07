// Spike M0.2: run inside the rootless netns (podman unshare --rootless-netns probe ...)
// and require K consecutive HTTP 2xx/3xx within a deadline. One fork per deploy.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	url := flag.String("url", "", "health URL")
	dns := flag.String("dns", "", "resolver (network gateway, aardvark-dns), e.g. 10.89.0.1:53")
	need := flag.Int("successes", 3, "consecutive successes required")
	every := flag.Duration("interval", time.Second, "probe interval")
	deadline := flag.Duration("timeout", 120*time.Second, "overall deadline")
	flag.Parse()

	d := &net.Dialer{Timeout: 2 * time.Second}
	if *dns != "" {
		d.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, "udp", *dns)
		}}
	}
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: d.DialContext, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start, ok := time.Now(), 0
	for time.Since(start) < *deadline {
		resp, err := c.Get(*url)
		if err == nil && resp.StatusCode < 400 {
			resp.Body.Close()
			ok++
			fmt.Printf("%6.2fs ok %d (%d/%d)\n", time.Since(start).Seconds(), resp.StatusCode, ok, *need)
			if ok == *need {
				return
			}
		} else {
			ok = 0
			if err == nil {
				resp.Body.Close()
				err = fmt.Errorf("status %d", resp.StatusCode)
			}
			fmt.Printf("%6.2fs fail: %v\n", time.Since(start).Seconds(), err)
		}
		time.Sleep(*every)
	}
	fmt.Println("unhealthy: deadline exceeded")
	os.Exit(1)
}
