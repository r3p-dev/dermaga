package telemetry

import (
	"sync"
	"time"
)

// ContainerMetric represents a point-in-time sample of container resource usage.
type ContainerMetric struct {
	Timestamp  time.Time `json:"timestamp"`
	Container  string    `json:"container"`
	CPUPercent float64   `json:"cpuPercent"`
	MemBytes   int64     `json:"memBytes"`
	MemLimit   int64     `json:"memLimit"`
	NetIn      int64     `json:"netIn"`
	NetOut     int64     `json:"netOut"`
	BlockIn    int64     `json:"blockIn"`
	BlockOut   int64     `json:"blockOut"`
}

// RingBuffer stores a fixed-capacity circular buffer of ContainerMetric samples
// per container, keeping daemon steady-state memory bounded and under budget.
type RingBuffer struct {
	mu       sync.RWMutex
	capacity int
	data     map[string][]ContainerMetric
	heads    map[string]int
	counts   map[string]int
}

// NewRingBuffer creates a ring buffer holding up to capacity items per container.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 120 // default 1 hour at 30s interval
	}
	return &RingBuffer{
		capacity: capacity,
		data:     make(map[string][]ContainerMetric),
		heads:    make(map[string]int),
		counts:   make(map[string]int),
	}
}

// Add appends a metric sample for its container.
func (rb *RingBuffer) Add(m ContainerMetric) {
	if m.Container == "" {
		return
	}
	rb.mu.Lock()
	defer rb.mu.Unlock()

	buf, ok := rb.data[m.Container]
	if !ok {
		buf = make([]ContainerMetric, rb.capacity)
		rb.data[m.Container] = buf
		rb.heads[m.Container] = 0
		rb.counts[m.Container] = 0
	}

	head := rb.heads[m.Container]
	buf[head] = m
	rb.heads[m.Container] = (head + 1) % rb.capacity
	if rb.counts[m.Container] < rb.capacity {
		rb.counts[m.Container]++
	}
}

// Get returns samples for a container recorded since the given time in chronological order.
func (rb *RingBuffer) Get(container string, since time.Time) []ContainerMetric {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	count, ok := rb.counts[container]
	if !ok || count == 0 {
		return nil
	}

	buf := rb.data[container]
	head := rb.heads[container]
	res := make([]ContainerMetric, 0, count)

	// Oldest item index in circular buffer
	start := (head - count + rb.capacity) % rb.capacity
	for i := 0; i < count; i++ {
		idx := (start + i) % rb.capacity
		item := buf[idx]
		if since.IsZero() || !item.Timestamp.Before(since) {
			res = append(res, item)
		}
	}
	return res
}

// Containers returns all container names tracked in the ring buffer.
func (rb *RingBuffer) Containers() []string {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	res := make([]string, 0, len(rb.data))
	for name := range rb.data {
		res = append(res, name)
	}
	return res
}

// GetAll returns recent metrics for all containers since the given time.
func (rb *RingBuffer) GetAll(since time.Time) map[string][]ContainerMetric {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	res := make(map[string][]ContainerMetric, len(rb.data))
	for name := range rb.data {
		count := rb.counts[name]
		if count == 0 {
			continue
		}
		buf := rb.data[name]
		head := rb.heads[name]
		start := (head - count + rb.capacity) % rb.capacity
		items := make([]ContainerMetric, 0, count)
		for i := 0; i < count; i++ {
			idx := (start + i) % rb.capacity
			item := buf[idx]
			if since.IsZero() || !item.Timestamp.Before(since) {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			res[name] = items
		}
	}
	return res
}
