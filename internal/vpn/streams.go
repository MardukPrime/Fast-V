package vpn

import (
	"io"
	"sync"
)

// streamRegistry tracks live relays so Close can tear all of them down at once.
// Adding after the registry has been closed reports false, which lets the caller
// drop the flow instead of leaking it past shutdown.
type streamRegistry struct {
	mu     sync.Mutex
	closed bool
	items  map[io.Closer]struct{}
}

func newStreamRegistry() *streamRegistry {
	return &streamRegistry{items: make(map[io.Closer]struct{})}
}

func (r *streamRegistry) add(item io.Closer) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.items[item] = struct{}{}
	return true
}

func (r *streamRegistry) remove(item io.Closer) {
	r.mu.Lock()
	delete(r.items, item)
	r.mu.Unlock()
}

func (r *streamRegistry) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

func (r *streamRegistry) closeAll() {
	r.mu.Lock()
	r.closed = true
	items := make([]io.Closer, 0, len(r.items))
	for item := range r.items {
		items = append(items, item)
	}
	r.items = make(map[io.Closer]struct{})
	r.mu.Unlock()

	for _, item := range items {
		_ = item.Close()
	}
}
