package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaultsAndOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
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
