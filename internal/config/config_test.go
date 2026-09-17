package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigPathAndTemplate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got, want := Path(), filepath.Join(dir, "gprm", "gprm_config.toml"); got != want {
		t.Fatalf("config path %q, want %q", got, want)
	}
	if err := WriteExample(Path()); err != nil {
		t.Fatal(err)
	}
	c, err := Load(Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Startup != "restore" || c.AutoQuit != "all-closed" || c.Interval != "5s" || c.Tools.GH != "gh" {
		t.Fatalf("unexpected shipped defaults: %+v", c)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Path(), filepath.Join(home, ".config", "gprm", "gprm_config.toml"); got != want {
		t.Fatalf("home config path %q, want %q", got, want)
	}
}

func TestConfigDefaultsAndOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gprm_config.toml")
	c, err := Load(path)
	if err != nil || c.Startup != "restore" || c.AutoQuit != "all-closed" {
		t.Fatalf("%+v %v", c, err)
	}
	if err = WriteExample(path); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Validate(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config not private")
	}
	if err = WriteExample(path); err == nil {
		t.Fatal("overwrote config")
	}
	os.WriteFile(path, []byte("startup='clipboard'\nauto_quit='never'\ninterval='12s'\n[tools]\ngh='/path with spaces/gh'\n"), 0600)
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Startup != "clipboard" || c.AutoQuit != "never" || c.Interval != "12s" || c.Timeout != "20s" {
		t.Fatal(c)
	}
	for _, mode := range []string{"restore", "clipboard", "empty", "auto-discover"} {
		c.Startup = mode
		if err = c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	c.Interval = "0s"
	if c.Validate() == nil {
		t.Fatal("zero interval accepted")
	}
	os.WriteFile(path, []byte("auto_quitt='never'"), 0600)
	if _, err = Load(path); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestCompletedRetentionSettings(t *testing.T) {
	c := Defaults()
	if c.CompletedRetention != "24h" {
		t.Fatal("unexpected retention default")
	}
	for _, v := range []string{"0s", "24h", "168h", "forever"} {
		c.CompletedRetention = v
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"", "-1h", "24d", "nonsense"} {
		c.CompletedRetention = v
		if c.Validate() == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}
