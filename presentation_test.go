package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/config"
)

func TestPresentationYAMLAndExplicitOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, yaml    string
		args          []string
		desc, compact bool
	}{
		{name: "omitted settings keep existing defaults", yaml: "default_sort: domain\n"},
		{name: "YAML defaults", yaml: "descending: true\ncompact: true\n", desc: true, compact: true},
		{name: "explicit false overrides YAML", yaml: "descending: true\ncompact: true\n", args: []string{"--desc=false", "--compact=false"}},
		{name: "explicit true overrides YAML", yaml: "descending: false\ncompact: false\n", args: []string{"--desc", "--compact"}, desc: true, compact: true},
		{name: "one override preserves other YAML default", yaml: "descending: true\ncompact: true\n", args: []string{"--compact=false"}, desc: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			flags.Bool("desc", false, "")
			flags.Bool("compact", false, "")
			if err := flags.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			desc, compact := presentationOptions(flags, cfg)
			if desc != tc.desc || compact != tc.compact {
				t.Fatalf("got (%v,%v), want (%v,%v)", desc, compact, tc.desc, tc.compact)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != tc.yaml {
				t.Fatal("configuration was modified")
			}
		})
	}
}

func TestPresentationRejectsInvalidYAMLBoolean(t *testing.T) {
	for _, content := range []string{"compact: surprise\n", "descending: surprise\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(path); err == nil {
			t.Fatal("invalid boolean accepted")
		}
	}
}
