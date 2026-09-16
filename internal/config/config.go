// Package config manages the user-editable TOML settings.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Tools struct {
	GH            string   `toml:"gh"`
	Clipboard     string   `toml:"clipboard"`
	ClipboardArgs []string `toml:"clipboard_args"`
	Open          string   `toml:"open"`
	OpenArgs      []string `toml:"open_args"`
}

type Jenkins struct {
	URL      string `toml:"url"`
	User     string `toml:"user"`
	Token    string `toml:"token"`
	UserEnv  string `toml:"user_env"`
	TokenEnv string `toml:"token_env"`
}

type Config struct {
	Startup        string    `toml:"startup"`
	AutoQuit       string    `toml:"auto_quit"`
	Interval       string    `toml:"interval"`
	Timeout        string    `toml:"request_timeout"`
	Sort           string    `toml:"sort"`
	Descending     bool      `toml:"descending"`
	CIColumn       string    `toml:"ci_column"`
	GitHubHost     string    `toml:"github_host"`
	DiscoveryLimit int       `toml:"discovery_limit"`
	Tools          Tools     `toml:"tools"`
	Jenkins        []Jenkins `toml:"jenkins"`
}

func Defaults() Config {
	return Config{Startup: "restore", AutoQuit: "all-closed", Interval: "5s", Timeout: "20s", Sort: "repo", CIColumn: "auto", GitHubHost: "github.com", DiscoveryLimit: 100, Tools: Tools{GH: "gh"}}
}

func Path() string {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "gprm", "config.toml")
}

func StatePath() string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(root, "gprm", "session.json")
}

func Load(path string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	meta, err := toml.Decode(string(b), &c)
	if err != nil {
		return c, fmt.Errorf("config: %w", err)
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return c, fmt.Errorf("unknown config setting: %s", keys[0])
	}
	return c, nil
}

func (c Config) Validate() error {
	for _, v := range []struct {
		name, value string
		allowed     []string
	}{
		{"startup", c.Startup, []string{"restore", "clipboard", "empty", "auto-discover"}},
		{"auto_quit", c.AutoQuit, []string{"never", "builds-finished", "all-passing", "all-closed"}},
		{"sort", c.Sort, []string{"repo", "progress"}},
		{"ci_column", c.CIColumn, []string{"auto", "always", "never"}},
	} {
		found := false
		for _, a := range v.allowed {
			if v.value == a {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s must be one of %s", v.name, strings.Join(v.allowed, ", "))
		}
	}
	for name, value := range map[string]string{"interval": c.Interval, "request_timeout": c.Timeout} {
		d, e := time.ParseDuration(value)
		if e != nil || d < time.Second {
			return fmt.Errorf("%s must be a duration of at least 1s", name)
		}
	}
	if c.Tools.GH == "" {
		return fmt.Errorf("tools.gh cannot be empty")
	}
	if c.DiscoveryLimit < 1 || c.DiscoveryLimit > 1000 {
		return fmt.Errorf("discovery_limit must be between 1 and 1000")
	}
	if c.GitHubHost == "" || strings.ContainsAny(c.GitHubHost, "/ :\t\n") {
		return fmt.Errorf("github_host must be a hostname")
	}
	for _, server := range c.Jenkins {
		u, err := url.Parse(server.URL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return fmt.Errorf("jenkins.url must be an HTTP(S) server URL without credentials, query, or fragment")
		}
	}
	return nil
}

func (c Config) PollInterval() time.Duration   { d, _ := time.ParseDuration(c.Interval); return d }
func (c Config) RequestTimeout() time.Duration { d, _ := time.ParseDuration(c.Timeout); return d }

func WriteExample(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(Example)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

const Example = `# gprm settings. Command-line flags override these defaults.
startup = "restore" # restore | clipboard | empty | auto-discover
auto_quit = "all-closed" # never | builds-finished | all-passing | all-closed
interval = "5s"
request_timeout = "20s"
sort = "repo" # repo | progress
descending = false
ci_column = "auto" # auto | always | never
github_host = "github.com"
discovery_limit = 100

[tools]
gh = "gh" # executable name or absolute path
# Empty clipboard/open paths select the platform default.
clipboard = ""
clipboard_args = []
open = ""
open_args = []
# macOS: clipboard = "/usr/bin/pbpaste", open = "/usr/bin/open"
# Wayland: clipboard = "wl-paste", clipboard_args = ["--no-newline"]
# X11: clipboard = "xclip", clipboard_args = ["-selection", "clipboard", "-o"]

# Add an entry for each Jenkins server. Credentials are sent only to its
# exact origin and URL path prefix. Cross-origin redirects are rejected.
# Environment values override inline credentials when set.
# [[jenkins]]
# url = "https://ci.example.com/jenkins"
# user = ""
# token = ""
# user_env = "JENKINS_USER"
# token_env = "JENKINS_API_TOKEN"
`
