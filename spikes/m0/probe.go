package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	dnsServer := flag.String("dns", "10.89.0.1:53", "DNS server to resolve container names (aardvark-dns)")
	timeout := flag.Duration("timeout", 5*time.Second, "probe timeout")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: probe [-dns ip:53] <url>\n")
		os.Exit(1)
	}
	targetURL := flag.Arg(0)

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "udp", *dnsServer)
		},
	}

	dialer := &net.Dialer{
		Resolver:  resolver,
		Timeout:   3 * time.Second,
		KeepAlive: -1,
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: 3 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   *timeout,
	}

	start := time.Now()
	resp, err := client.Get(targetURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "PROBE ERROR: %v\n", err)
		os.Exit(2)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	duration := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "READ ERROR: %v\n", err)
		os.Exit(3)
	}

	fmt.Printf("HTTP %d (%s) in %v\n", resp.StatusCode, resp.Status, duration)
	fmt.Printf("Body: %s\n", string(body))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		os.Exit(0)
	}
	os.Exit(1)
}
