package doctor

import (
	"os/user"
	"testing"
	"time"
)

func TestSubIDCount(t *testing.T) {
	u := &user.User{Username: "testuser", Uid: "1000"}
	data := `root:100000:65536
testuser:165536:65536
other:231072:65536
`
	if n := SubIDCount(data, u); n != 65536 {
		t.Fatalf("expected 65536, got %d", n)
	}

	u2 := &user.User{Username: "nonexistent", Uid: "9999"}
	if n := SubIDCount(data, u2); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestClockCheck(t *testing.T) {
	c1 := ClockCheck(true, 10*time.Minute)
	if c1.Status != "ok" {
		t.Fatalf("expected ok, got %s", c1.Status)
	}

	c2 := ClockCheck(false, 2*time.Minute)
	if c2.Status != "warn" {
		t.Fatalf("expected warn, got %s", c2.Status)
	}

	c3 := ClockCheck(false, 10*time.Minute)
	if c3.Status != "fail" {
		t.Fatalf("expected fail, got %s", c3.Status)
	}
}

func TestRunHostChecks(t *testing.T) {
	checks := RunHostChecks()
	if len(checks) == 0 {
		t.Fatal("expected at least 1 check")
	}
}
