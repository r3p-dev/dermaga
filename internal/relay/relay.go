// Package relay implements `dermaga hook-relay` (ARCHITECTURE D1): it runs in
// the dg-hooks container on network "web" and forwards every TCP connection
// byte for byte to the daemon's bind-mounted hooks.sock. It parses nothing;
// all validation stays in the daemon. Ported from the M0 spike (spikes/m0/relay at eb9ad64).
package relay

import (
	"errors"
	"io"
	"log"
	"net"
	"time"
)

// Serve accepts on ln until ln is closed.
func Serve(ln net.Listener, target string) error {
	for {
		c, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return err
		}
		if err != nil {
			log.Print(err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go pipe(c, target)
	}
}

type halfCloser interface{ CloseWrite() error }

func pipe(c net.Conn, target string) {
	defer c.Close()
	u, err := net.DialTimeout("unix", target, 2*time.Second)
	if err != nil {
		log.Print(err)
		return
	}
	defer u.Close()
	done := make(chan struct{}, 2)
	// Half-close each direction when its source ends, so an EOF from either
	// side reaches the other instead of leaving the connection hanging.
	copyHalf := func(dst, src net.Conn) {
		io.Copy(dst, src)
		if hc, ok := dst.(halfCloser); ok {
			hc.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyHalf(u, c)
	go copyHalf(c, u)
	<-done
	<-done
}
