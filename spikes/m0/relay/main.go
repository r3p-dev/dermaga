// Spike M0.1: TCP -> Unix socket relay, run as a container on network "web",
// so Traefik reaches the daemon's hook listener without any host TCP port.
//
//	relay -listen :8080 -target /run/dermaga/hooks.sock   (inside the container)
//	relay -serve /run/user/1001/dermaga/hooks.sock        (stand-in for the daemon)
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	listen := flag.String("listen", ":8080", "TCP address to accept on")
	target := flag.String("target", "/run/dermaga/hooks.sock", "Unix socket to forward to")
	serve := flag.String("serve", "", "run a test HTTP server on this Unix socket instead")
	flag.Parse()

	if *serve != "" {
		os.Remove(*serve)
		ln, err := net.Listen("unix", *serve)
		if err != nil {
			log.Fatal(err)
		}
		os.Chmod(*serve, 0o600)
		log.Fatal(http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "daemon got %s %s host=%s xff=%q\n", r.Method, r.URL.Path, r.Host, r.Header.Get("X-Forwarded-For"))
		})))
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Print(err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go pipe(c, *target)
	}
}

func pipe(c net.Conn, target string) {
	defer c.Close()
	u, err := net.DialTimeout("unix", target, 2*time.Second)
	if err != nil {
		log.Print(err)
		return
	}
	defer u.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(u, c); u.(*net.UnixConn).CloseWrite(); done <- struct{}{} }()
	go func() { io.Copy(c, u); done <- struct{}{} }()
	<-done
	<-done
}
