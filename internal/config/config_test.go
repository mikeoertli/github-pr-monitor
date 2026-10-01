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
	os.WriteFile(path, []byte("startup='clipboard'\nauto_quit='never'\ninterval='12s'\ntarget_branch_ignored_prefixes=['support/']\n[tools]\ngh='/path with spaces/gh'\n"), 0600)
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Startup != "clipboard" || c.AutoQuit != "never" || c.Interval != "12s" || c.Timeout != "20s" || len(c.TargetBranchIgnoredPrefixes) != 1 || c.TargetBranchIgnoredPrefixes[0] != "support/" {
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

func TestJiraConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gprm_config.toml")
	if err := os.WriteFile(path, []byte("sort='jira'\n[jira]\nbase_url='https://jira.example.com/team/'\nproject_prefixes=['ABC','ops']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := c.Jira.URL("ABC-42"); got != "https://jira.example.com/team/browse/ABC-42" {
		t.Fatal(got)
	}
	if c.Jira.URL("") != "" || (Jira{}).URL("ABC-42") != "" {
		t.Fatal("link without ticket/server")
	}
	for _, base := range []string{"file:///tmp/jira", "https://user:secret@jira.example.com", "https://jira.example.com?q=x", "https://jira.example.com#x", "jira.example.com"} {
		bad := c
		bad.Jira.BaseURL = base
		if bad.Validate() == nil {
			t.Errorf("accepted %s", base)
		}
	}
	for _, prefix := range []string{"", "ABC-1", "A B", "2ABC", "A/B"} {
		bad := c
		bad.Jira.ProjectPrefixes = []string{prefix}
		if bad.Validate() == nil {
			t.Errorf("accepted prefix %q", prefix)
		}
	}
}

func TestCompoundSortConfig(t *testing.T) {
	c := Defaults()
	for _, spec := range []string{"repo", "progress", "jira", "jira,target,repo", "jira,repo,target", "target,repo", "target, progress, repo"} {
		c.Sort = spec
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
	}
	for _, spec := range []string{"", "jira,,repo", "repo,repo", "repo,jira", "unknown"} {
		c.Sort = spec
		if c.Validate() == nil {
			t.Fatalf("accepted %q", spec)
		}
	}
}
