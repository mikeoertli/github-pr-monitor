package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
)

func TestPrintConfigOffline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("invalid TOML with secret-token"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := Run([]string{"--print-config", "--config", path, "--gh", "/missing/gh"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if out.String() != config.Example || strings.Contains(out.String(), "secret-token") {
		t.Fatal("did not print only documented defaults")
	}
	// Every Config field (including nested/server fields) must appear in the reference.
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := f.Tag.Get("toml")
			if !strings.Contains(out.String(), name) {
				t.Fatalf("undocumented setting %s", name)
			}
			nested := f.Type
			if nested.Kind() == reflect.Slice {
				nested = nested.Elem()
			}
			if nested.Kind() == reflect.Struct {
				check(nested)
			}
		}
	}
	check(reflect.TypeOf(config.Config{}))
}

func TestConfigEditorHelper(t *testing.T) {
	mode := os.Getenv("GPRM_TEST_EDITOR")
	if mode == "" {
		return
	}
	path := os.Args[len(os.Args)-1]
	b, err := os.ReadFile(path)
	if err != nil {
		os.Exit(2)
	}
	if mode == "fail" {
		os.Exit(7)
	}
	if mode == "invalid" {
		if os.WriteFile(path, []byte("sort='invalid'\n"), 0600) != nil {
			os.Exit(3)
		}
	} else {
		// Confirms both quoted flags and the config path arrive as separate arguments.
		if os.Args[len(os.Args)-2] != "argument with spaces" {
			os.Exit(4)
		}
		fmt.Fprint(os.Stdout, string(b))
		if os.WriteFile(path, []byte("sort='jira,target,repo'\n"), 0600) != nil {
			os.Exit(5)
		}
	}
	os.Exit(0)
}

func TestEditConfigCreatesPreservesAndValidates(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", fmt.Sprintf("%q -test.run=^TestConfigEditorHelper$ -- 'argument with spaces'", exe))
	path := filepath.Join(t.TempDir(), "with spaces; literal", "config.toml")
	for _, mode := range []string{"new", "existing", "invalid", "fail"} {
		t.Setenv("GPRM_TEST_EDITOR", mode)
		if mode == "existing" {
			if err := os.WriteFile(path, []byte("# keep existing content\nsort='repo'\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var out, stderr bytes.Buffer
		err := Run([]string{"--edit-config", "--config", path, "--gh", "/missing/gh"}, &out, &stderr)
		if mode == "new" || mode == "existing" {
			if err != nil {
				t.Fatal(err)
			}
			want := "# keep existing content"
			if mode == "new" {
				want = config.Example
			}
			if !strings.Contains(out.String(), want) {
				t.Fatalf("editor did not receive original config: %q", out.String())
			}
			c, e := config.Load(path)
			if e != nil || c.Sort != "jira,target,repo" {
				t.Fatal(c, e)
			}
		} else if err == nil {
			t.Fatalf("%s error hidden", mode)
		}
	}
}

func TestEditorArgs(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{`code --wait`, []string{"code", "--wait"}},
		{`'/path with spaces/editor' --flag "two words"`, []string{"/path with spaces/editor", "--flag", "two words"}},
		{`editor '$(touch nothing)'`, []string{"editor", "$(touch nothing)"}},
	} {
		got, err := editorArgs(tc.raw)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got, err)
		}
	}
	for _, raw := range []string{"", `"unfinished`, `editor\`} {
		if _, err := editorArgs(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestDefaultSortAndCLIOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("startup='empty'\nsort='jira,target,repo'\n[tools]\ngh='/missing/gh'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"", "target,repo"} {
		var out, stderr bytes.Buffer
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"--once", "--config", path, "--state", filepath.Join(t.TempDir(), "session.json"), "--gh", exe}
		want := "jira,target,repo"
		if override != "" {
			args = append(args, "--sort", override)
			want = override
		}
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "sort "+want) {
			t.Fatal(out.String())
		}
	}
	var out, stderr bytes.Buffer
	if err := Run([]string{"--demo", "--once", "--sort", "jira,target,repo"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "── ABC-42 ──") {
		t.Fatal(out.String())
	}
}
