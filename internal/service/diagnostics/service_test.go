package diagnostics

import (
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

type fakeProxyState struct {
	state model.ProxyState
}

func (f fakeProxyState) State() model.ProxyState {
	return f.state
}

func TestCheckRejectsInvalidMode(t *testing.T) {
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}})
	if _, err := service.Check("invalid", false); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestCheckRejectsStoppedProxy(t *testing.T) {
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}})
	if _, err := service.Check("proxy", false); err == nil {
		t.Fatal("expected stopped proxy error")
	}
}
