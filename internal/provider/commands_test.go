package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestRequestCommandHelper(t *testing.T) {
	if os.Getenv("GPRM_COMMAND_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg != "--" {
			continue
		}
		b, _ := json.Marshal(os.Args[i+1:])
		f, err := os.OpenFile(os.Getenv("GPRM_COMMAND_CAPTURE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(2)
		}
		f.Write(append(b, '\n'))
		f.Close()
		os.Exit(0)
	}
	os.Exit(3)
}

// Run copied commands against a fake executable, never the clipboard or network.
func commandHarness(t *testing.T) (string, func(string, string) [][]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("copied commands target POSIX shells")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake gh's executable")
	script := "#!/bin/sh\nexec " + shellQuote(os.Args[0]) + " -test.run=^TestRequestCommandHelper$ -- \"$@\"\n"
	for _, path := range []string{bin, filepath.Join(dir, "curl")} {
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GPRM_COMMAND_HELPER", "1")
	capture := filepath.Join(dir, "arguments.jsonl")
	t.Setenv("GPRM_COMMAND_CAPTURE", capture)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin, func(shell, command string) [][]string {
		t.Helper()
		os.WriteFile(capture, nil, 0600)
		if out, err := exec.Command(shell, "-c", command).CombinedOutput(); err != nil {
			t.Fatalf("copied command failed: %v %s", err, out)
		}
		b, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		var calls [][]string
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var args []string
			if err := json.Unmarshal([]byte(line), &args); err != nil {
				t.Fatal(err)
			}
			calls = append(calls, args)
		}
		return calls
	}
}

func TestCopiedGitHubCommandsReplayRequests(t *testing.T) {
	bin, run := commandHarness(t)
	c := config.Defaults()
	c.Tools.GH = bin
	g := New(c)
	ref, _ := core.ParseRef("https://github.example.com/acme/api/pull/23", "github.com")
	var actual [][]string
	g.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		actual = append(actual, append([]string(nil), args...))
		if len(actual) == 1 {
			return graph("head", "OPEN", nil, true, "cursor'$(false)"), nil
		}
		return graph("head", "OPEN", nil, false, ""), nil
	})
	p := g.Fetch(context.Background(), ref)
	if p.Error != "" || !reflect.DeepEqual(p.GitHubRequests, actual) {
		t.Fatal("requests were not recorded")
	}
	command := GitHubCommand(c, p)
	for _, shell := range []string{"/bin/sh", "/bin/bash", "/bin/zsh"} {
		if _, err := os.Stat(shell); err != nil {
			continue
		}
		if got := run(shell, command); !reflect.DeepEqual(got, actual) {
			t.Fatalf("%s changed gh arguments", shell)
		}
	}
	if got := run("/bin/sh", GitHubCommand(c, core.NewPR(ref, p.LastSuccess))); !reflect.DeepEqual(got, [][]string{githubArgs(ref, "")}) {
		t.Fatal("pre-refresh command differs from monitor query")
	}
}

func argValue(args []string, key string) string {
	for i, arg := range args {
		if arg == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestCopiedCurlCredentialsAndSourceRequests(t *testing.T) {
	_, run := commandHarness(t)
	c := config.Defaults()
	c.Timeout = "7s"
	c.Jenkins = []config.Jenkins{{URL: "https://ci.example.com/jenkins", User: "inline ' user", Token: "token'\"$() `literal`\nend", UserEnv: "GPRM_COPY_USER", TokenEnv: "GPRM_COPY_TOKEN"}}
	job := core.Job{Provider: "Jenkins", URL: "https://ci.example.com/jenkins/job/a/8/", Status: "running"}
	job.JenkinsRequests = []string{job.URL + "api/json", estimateURL(strings.TrimRight(job.URL, "/")), job.URL + "wfapi/describe", "https://ci.example.com/outside/api/json"}
	for _, env := range []bool{false, true} {
		user, token := "", ""
		if env {
			user, token = "env user", "env token'$(false)"
		}
		t.Setenv("GPRM_COPY_USER", user)
		t.Setenv("GPRM_COPY_TOKEN", token)
		command, err := JenkinsCommand(c, job)
		if err != nil {
			t.Fatal(err)
		}
		if env && strings.Contains(command, token) {
			t.Fatal("expanded environment secret into clipboard")
		}
		for _, shell := range []string{"/bin/sh", "/bin/bash", "/bin/zsh"} {
			if _, err := os.Stat(shell); err != nil {
				continue
			}
			calls := run(shell, command)
			if len(calls) != len(job.JenkinsRequests) {
				t.Fatal("missing source request")
			}
			j := NewJenkins(c)
			for i, args := range calls {
				endpoint := job.JenkinsRequests[i]
				if argValue(args, "--url") != endpoint || argValue(args, "--max-time") != "7" || argValue(args, "--header") != "Accept: application/json" || args[0] != "--disable" {
					t.Fatal("curl differs from request settings")
				}
				j.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					u, p, auth := req.BasicAuth()
					want := ""
					if auth {
						want = u + ":" + p
					}
					if argValue(args, "--user") != want {
						t.Fatal("curl authentication differs from HTTP client")
					}
					return response(200, `{}`), nil
				})
				var out any
				if err := j.get(context.Background(), endpoint, &out); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	c.Jenkins[0].UserEnv = "BAD;exit"
	if _, err := JenkinsCommand(c, job); err == nil {
		t.Fatal("accepted unsafe environment name")
	}
	for _, job := range []core.Job{{Provider: "GitHub Actions", URL: "https://github.com/a/b/actions/runs/1"}, {Provider: "Jenkins", URL: "https://ci.example.com/blue/organizations/jenkins/a"}, {Provider: "Jenkins", URL: "file:///job/a/1/"}} {
		if _, err := JenkinsCommand(config.Defaults(), job); err == nil {
			t.Fatal("accepted non-Jenkins/direct URL")
		}
	}
}
