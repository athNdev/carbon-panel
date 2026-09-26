package provision

import (
	"sync"
)

// LogBuffer is a bounded ring buffer with subscriber fan-out. Appends beyond
// cap evict the oldest line; new subscribers replay the retained lines then
// receive live ones.
type LogBuffer struct {
	mu    sync.Mutex
	cap   int
	lines []string
	subs  map[chan string]struct{}
}

// NewLogBuffer returns a buffer retaining the last n lines (n<=0 -> 500).
func NewLogBuffer(n int) *LogBuffer {
	if n <= 0 {
		n = 500
	}
	return &LogBuffer{cap: n, subs: map[chan string]struct{}{}}
}

// Append adds a line, evicting the oldest when full, and fans out to live
// subscribers without blocking the writer.
func (b *LogBuffer) Append(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) >= b.cap {
		copy(b.lines, b.lines[1:])
		b.lines[len(b.lines)-1] = line
	} else {
		b.lines = append(b.lines, line)
	}
	for ch := range b.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// Subscribe replays retained lines and returns a live channel. Call the
// returned cancel to unsubscribe and close the channel.
func (b *LogBuffer) Subscribe() (replay []string, ch <-chan string, cancel func()) {
	c := make(chan string, 64)
	b.mu.Lock()
	replay = append([]string(nil), b.lines...)
	b.subs[c] = struct{}{}
	b.mu.Unlock()
	return replay, c, func() {
		b.mu.Lock()
		delete(b.subs, c)
		b.mu.Unlock()
		close(c)
	}
}

// Len returns the retained line count.
func (b *LogBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
}

// LogRegistry holds one buffer per provision id.
type LogRegistry struct {
	mu   sync.Mutex
	size int
	bufs map[string]*LogBuffer
}

// NewLogRegistry returns a registry whose buffers retain size lines each.
func NewLogRegistry(size int) *LogRegistry {
	if size <= 0 {
		size = 500
	}
	return &LogRegistry{size: size, bufs: map[string]*LogBuffer{}}
}

func (r *LogRegistry) For(id string) *LogBuffer {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bufs[id]
	if !ok {
		b = NewLogBuffer(r.size)
		r.bufs[id] = b
	}
	return b
}

// Append adds a line to id's buffer, creating it on first use.
func (r *LogRegistry) Append(id, line string) { r.For(id).Append(line) }
