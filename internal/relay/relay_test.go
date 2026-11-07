package relay

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelayForwardsHTTPToUnixSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "hooks.sock")
	uln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	daemon := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s %s host=%s body=%s", r.Method, r.URL.Path, r.Host, b)
	})}
	go daemon.Serve(uln)
	defer daemon.Close()

	tln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go Serve(tln, sock)
	defer tln.Close()

	c := &http.Client{}
	for i := range 3 { // keep-alive: several requests over the relayed connection
		resp, err := c.Post("http://"+tln.Addr().String()+"/hooks/github/abc", "application/json", strings.NewReader(fmt.Sprint(i)))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if want := fmt.Sprintf("POST /hooks/github/abc host=%s body=%d", tln.Addr(), i); string(b) != want {
			t.Fatalf("got %q want %q", b, want)
		}
	}

	// target down: the client's connection is closed, not left hanging
	daemon.Close()
	if _, err := http.Get("http://" + tln.Addr().String() + "/"); err == nil {
		t.Fatal("request succeeded with the daemon down")
	}
}
