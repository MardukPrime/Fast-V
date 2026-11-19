//go:build embeddedcore

package embedded

import (
	"errors"

	"github.com/xtls/xray-core/core"
	_ "github.com/xtls/xray-core/main/distro/all"
)

// Instance wraps one in-process Xray-core instance.
type Instance struct {
	inner *core.Instance
}

// Start parses the generated JSON config and starts Xray-core in this process.
// Unlike exec-ing the core binary, no config path, working directory or CLI
// flags are involved: the manager's existing config builder output is passed
// straight in.
func Start(config []byte) (*Instance, error) {
	if len(config) == 0 {
		return nil, errors.New("embedded core: empty config")
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
