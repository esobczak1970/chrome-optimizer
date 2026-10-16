package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"valid", "cdp_endpoint: http://127.0.0.1:9222\ntimeout: 8s\ndefault_sort: title\n", ""},
		{"unknown", "cdp_endpoint: http://127.0.0.1:9222\nsecrets: no\n", "field secrets not found"},
		{"bad duration", "timeout: -3s\n", "timeout"},
		{"bad sort", "default_sort: memory\n", "default_sort"},
		{"empty", "", "empty"},
		{"multiple", "timeout: 5s\n---\ndefault_sort: title\n", "exactly one document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v; want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got.DefaultSort != "title" || got.Timeout != "8s" {
				t.Fatalf("got %v, error %v", got, err)
			}
		})
	}
}
