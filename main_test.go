package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLifecycleCLIRejectsUnsafeFlagsBeforeNetwork(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"freeze requires consent", []string{"--freeze=EXACT_ID"}, "--confirm-freeze"},
		{"confirm alone", []string{"--confirm-freeze"}, "requires --freeze"},
		{"empty freeze selector", []string{"--freeze=", "--confirm-freeze"}, "requires a complete TargetID"},
		{"empty resume selector", []string{"--resume="}, "requires a complete TargetID"},
		{"mutually exclusive", []string{"--freeze=TARGET_A", "--resume=TARGET_B", "--confirm-freeze"}, "mutually exclusive"},
		{"watch freeze", []string{"--freeze=TARGET_A", "--confirm-freeze", "--watch"}, "read-only"},
		{"watch resume", []string{"--resume=TARGET_A", "--watch"}, "read-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			var stderr bytes.Buffer
			// The missing YAML file intentionally proves that the action
			// is rejected before config parsing or any Chrome connection.
			args := append([]string{"--config=/nonexistent/settings.yaml"}, tt.args...)
			err := run(context.Background(), args, &out, &stderr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, expected substring %q", err, tt.want)
			}
			if out.Len() != 0 {
				t.Fatalf("mutating command produced output before validation: %q", out.String())
			}
		})
	}
}
