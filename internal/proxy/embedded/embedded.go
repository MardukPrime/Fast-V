//go:build embeddedcore || android

package embedded

import (
	"errors"
	"strings"

	"github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/core"
	_ "github.com/xtls/xray-core/main/distro/all"
)

// Sink receives one core log line. level is "error", "warning", "info" or
// "debug".
type Sink func(level, message string)

// Instance wraps one in-process Xray-core instance.
type Instance struct {
	inner *core.Instance
}

// Start parses the generated JSON config and starts Xray-core in this process.
// Unlike exec-ing the core binary, no config path, working directory or CLI
// flags are involved: the manager's existing config builder output is passed
// straight in.
func Start(config []byte) (*Instance, error) {
	return StartWithSink(config, nil)
}

// StartWithSink is Start plus a log sink. The in-process core writes its log to
// stdout, which a GUI app — on Android especially — does not have, so its
// diagnostics would be dropped. Forwarding them into the app log keeps core
// errors (dial failures, TLS handshakes, rejected users) visible next to the
// manager's own messages.
func StartWithSink(config []byte, sink Sink) (*Instance, error) {
	if len(config) == 0 {
		return nil, errors.New("embedded core: empty config")
	}
	if sink != nil {
		log.RegisterHandler(&sinkHandler{sink: sink})
	}
	instance, err := core.StartInstance("json", config)
	if err != nil {
		return nil, err
	}
	return &Instance{inner: instance}, nil
}

// Close stops the in-process core.
func (i *Instance) Close() error {
	if i == nil || i.inner == nil {
		return nil
	}
	return i.inner.Close()
}

// sinkHandler adapts the core's global log handler to a Sink.
type sinkHandler struct {
	sink Sink
}

func (h *sinkHandler) Handle(msg log.Message) {
	if h == nil || h.sink == nil || msg == nil {
		return
	}
	text := strings.TrimSpace(msg.String())
	if text == "" {
		return
	}
	level := "info"
	switch typed := msg.(type) {
	case *log.GeneralMessage:
		level = severityLevel(typed.Severity)
	case *log.AccessMessage:
		// One line per connection: useful when debugging, too noisy by default.
		level = "debug"
	}
	h.sink(level, text)
}

func severityLevel(severity log.Severity) string {
	switch severity {
	case log.Severity_Error:
		return "error"
	case log.Severity_Warning:
		return "warning"
	case log.Severity_Debug:
		return "debug"
	default:
		return "info"
	}
}
