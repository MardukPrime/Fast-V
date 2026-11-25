//go:build embeddedcore || android

package embedded

import (
	"testing"

	"github.com/xtls/xray-core/common/log"
)

func TestSinkHandlerMapsSeverityAndContent(t *testing.T) {
	var gotLevel, gotText string
	handler := &sinkHandler{sink: func(level, message string) {
		gotLevel, gotText = level, message
	}}

	handler.Handle(&log.GeneralMessage{Severity: log.Severity_Warning, Content: "failed to dial"})
	if gotLevel != "warning" {
		t.Fatalf("level = %q, want warning", gotLevel)
	}
	if gotText == "" || gotText == "failed to dial" {
		t.Fatalf("message should keep the severity prefix, got %q", gotText)
	}

	handler.Handle(&log.GeneralMessage{Severity: log.Severity_Error, Content: "boom"})
	if gotLevel != "error" {
		t.Fatalf("level = %q, want error for an error message", gotLevel)
	}

	handler.Handle(&log.GeneralMessage{Severity: log.Severity_Debug, Content: "trace"})
	if gotLevel != "debug" {
		t.Fatalf("level = %q, want debug", gotLevel)
	}

	// Access logs are per-connection noise, kept at debug level.
	handler.Handle(&log.AccessMessage{To: "tcp:example.com:443"})
	if gotLevel != "debug" {
		t.Fatalf("access log level = %q, want debug", gotLevel)
	}
}

func TestStartWithSinkRejectsEmptyConfig(t *testing.T) {
	if _, err := StartWithSink(nil, func(string, string) {}); err == nil {
		t.Fatal("expected an error for an empty config")
	}
}
