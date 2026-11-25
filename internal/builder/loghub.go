package builder

import (
	"strings"
	"sync"
)

const maxLogBytes = 512 * 1024 // Bounded per-deployment log buffer (512 KiB).

// LogHub manages bounded build log buffers and live SSE subscribers per deployment (DEP-8, LOG-2).
type LogHub struct {
	mu   sync.Mutex
	logs map[string]*deployLog
}

type deployLog struct {
	buf  strings.Builder
	done bool
	subs map[chan string]struct{}
}

// NewLogHub initializes a LogHub.
func NewLogHub() *LogHub {
	return &LogHub{
		logs: make(map[string]*deployLog),
	}
}

func (h *LogHub) getOrCreate(depID string) *deployLog {
	dl, ok := h.logs[depID]
	if !ok {
		dl = &deployLog{subs: make(map[chan string]struct{})}
		h.logs[depID] = dl
	}
	return dl
}

// Append appends a masked log line (adding a trailing newline if absent) and notifies subscribers.
func (h *LogHub) Append(depID, line string) {
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	dl := h.getOrCreate(depID)
	if dl.buf.Len()+len(line) > maxLogBytes {
		cur := dl.buf.String()
		keepFrom := len(cur) / 2
		if idx := strings.IndexByte(cur[keepFrom:], '\n'); idx >= 0 {
			keepFrom += idx + 1
		}
		dl.buf.Reset()
		dl.buf.WriteString("[...truncated...]\n")
		dl.buf.WriteString(cur[keepFrom:])
	}
	dl.buf.WriteString(line)
	for ch := range dl.subs {
		select {
		case ch <- line:
		default:
			// Slow subscriber: drop rather than blocking the build.
		}
	}
}

// Finish marks a deployment log stream as complete and closes active subscriber channels.
func (h *LogHub) Finish(depID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	dl := h.getOrCreate(depID)
	if dl.done {
		return
	}
	dl.done = true
	for ch := range dl.subs {
		close(ch)
		delete(dl.subs, ch)
	}
}

// Snapshot returns the current buffered log text for a deployment.
func (h *LogHub) Snapshot(depID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	dl, ok := h.logs[depID]
	if !ok {
		return ""
	}
	return dl.buf.String()
}

// Subscribe returns the current snapshot, a channel of subsequent lines (closed when finished),
// whether the stream is already done, and a cleanup function.
func (h *LogHub) Subscribe(depID string) (snapshot string, ch <-chan string, done bool, cancel func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	dl := h.getOrCreate(depID)
	snapshot = dl.buf.String()
	if dl.done {
		closed := make(chan string)
		close(closed)
		return snapshot, closed, true, func() {}
	}
	sub := make(chan string, 64)
	dl.subs[sub] = struct{}{}
	return snapshot, sub, false, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := dl.subs[sub]; ok {
			delete(dl.subs, sub)
			close(sub)
		}
	}
}

// Delete removes in-memory log state for a deployment.
func (h *LogHub) Delete(depID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if dl, ok := h.logs[depID]; ok {
		for ch := range dl.subs {
			close(ch)
		}
		delete(h.logs, depID)
	}
}
