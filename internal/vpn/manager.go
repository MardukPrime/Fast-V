package vpn

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// ErrAlreadyRunning is returned when a session is started while another one is
// already active. Android allows a single VpnService interface per app.
var ErrAlreadyRunning = errors.New("vpn session already running")

// Manager owns the one tun session an app process can have at a time and is the
// seam the Android service calls into.
type Manager struct {
	mu      sync.Mutex
	session *Session
	device  *os.File
	cfg     Config
}

// Start takes ownership of fd (the tun descriptor from VpnService.establish) and
// begins forwarding its traffic through the SOCKS5 inbound in cfg.
//
// The descriptor is closed when the session stops, so the caller must not close
// it again (Android: use ParcelFileDescriptor.detachFd()).
func (m *Manager) Start(fd int, cfg Config) (Stats, error) {
	if fd < 0 {
		return Stats{}, fmt.Errorf("invalid tun descriptor %d", fd)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session != nil {
		return Stats{}, ErrAlreadyRunning
	}

	device := os.NewFile(uintptr(fd), "fastv-tun")
	if device == nil {
		return Stats{}, fmt.Errorf("wrap tun descriptor %d", fd)
	}
	session, err := Start(device, cfg)
	if err != nil {
		_ = device.Close()
		return Stats{}, err
	}
	m.session = session
	m.device = device
	m.cfg = cfg.withDefaults()
	return session.Stats(), nil
}

// Stop tears the session down. It is a no-op when nothing is running.
func (m *Manager) Stop() error {
	m.mu.Lock()
	session := m.session
	m.session = nil
	m.device = nil
	m.cfg = Config{}
	m.mu.Unlock()

	if session == nil {
		return nil
	}
	return session.Close()
}

// Status reports the counters of the running session.
func (m *Manager) Status() (Stats, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return Stats{}, false
	}
	return m.session.Stats(), true
}
