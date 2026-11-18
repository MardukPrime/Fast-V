//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
)

// SystemProxyState is the current user's WinINet proxy configuration. It is
// only meaningful on Windows; the other functions in this file report the
// platform as unsupported.
type SystemProxyState struct {
	Enabled  bool
	Server   string
	Override string
	// AutoConfigURL mirrors the Windows PAC script URL field; it is always
	// empty on other platforms.
	AutoConfigURL string
}

// ReadSystemProxyState is unsupported outside Windows.
func ReadSystemProxyState() (SystemProxyState, error) {
	return SystemProxyState{}, errors.New("仅支持 Windows")
}

// ReadWinHTTPProxy is unsupported outside Windows.
func ReadWinHTTPProxy() (string, error) {
	return "", errors.New("仅支持 Windows")
}

// ResetSystemProxy is unsupported outside Windows.
func ResetSystemProxy() error {
	return errors.New("仅支持 Windows")
}

// FlushDNS is unsupported outside Windows.
func FlushDNS() error {
	return errors.New("仅支持 Windows")
}

// PortInUse reports whether 127.0.0.1:port is already bound by another process.
// It only uses the net standard library, so it builds on every platform.
func PortInUse(port int) (bool, error) {
	if port < 1 || port > 65535 {
		return false, fmt.Errorf("invalid port: %d", port)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil {
		_ = listener.Close()
		return false, nil
	}
	if isAddressInUse(err) {
		return true, nil
	}
	return false, fmt.Errorf("check port %d: %w", port, err)
}

// isAddressInUse reports whether a listen error means the address is already
// bound.
func isAddressInUse(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// 10048 = WSAEADDRINUSE, 98 = Linux EADDRINUSE.
		if int(errno) == 10048 || int(errno) == 98 {
			return true
		}
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "address already in use") ||
		strings.Contains(message, "only one usage of each socket address")
}
