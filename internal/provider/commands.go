package provider

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

type requestRecorder struct {
	Runner
	requests *[][]string
}

func (r requestRecorder) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	*r.requests = append(*r.requests, append([]string(nil), args...))
	return r.Runner.Run(ctx, path, args...)
}

func githubArgs(ref core.Ref, cursor string) []string {
	parts := strings.SplitN(ref.Repo, "/", 2)
	args := []string{"api", "graphql", "--hostname", ref.Host, "-f", "query=" + query, "-f", "owner=" + parts[0], "-f", "repo=" + parts[1], "-F", "number=" + strconv.Itoa(ref.Number)}
	if cursor != "" {
		args = append(args, "-f", "cursor="+cursor)
	}
	return args
}

// shellQuote produces a literal argument for POSIX shells, including bash/zsh.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func shellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, s := range args {
		quoted[i] = shellQuote(s)
	}
	return strings.Join(quoted, " ")
}

// GitHubCommand replays the actual pages and Actions requests for the last update.
func GitHubCommand(c config.Config, p core.PR) string {
	requests := p.GitHubRequests
	if len(requests) == 0 {
		requests = [][]string{githubArgs(p.Ref, "")}
	}
	var commands []string
	for _, args := range requests {
		commands = append(commands, "GH_PROMPT_DISABLED=1 GH_PAGER=cat NO_COLOR=1 "+shellCommand(append([]string{c.Tools.GH}, args...)))
	}
	return "(\n" + strings.Join(commands, "\n") + "\n)\n"
}

func jenkinsServer(c config.Config, raw string) *config.Jenkins {
	for i := range c.Jenkins {
		if under(raw, c.Jenkins[i].URL) {
			return &c.Jenkins[i]
		}
	}
	return nil
}
func jenkinsBuildRoot(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || core.BuildURLWarning(raw) != "" || !buildPath.MatchString(u.Path) || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("selected check has no direct Jenkins build URL; copy the gh command for its GitHub status")
	}
	return strings.TrimRight(raw, "/"), nil
}
func estimateURL(raw string) string {
	return raw[:strings.LastIndex(raw, "/")] + "/lastSuccessfulBuild/api/json"
}

var shellEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// credentialAssignment keeps environment secrets symbolic, with the same
// nonempty-environment-over-inline precedence as the HTTP client.
func credentialAssignment(local, name, fallback string) (string, error) {
	s := local + "=" + shellQuote(fallback) + "\n"
	if name != "" {
		if !shellEnvName.MatchString(name) {
			return "", fmt.Errorf("configured Jenkins environment variable name is not valid for a shell command")
		}
		s += "if [ -n \"${" + name + ":-}\" ]; then " + local + "=\"${" + name + "}\"; fi\n"
	}
	return s, nil
}

// JenkinsCommand copies source requests, including a cached estimate's source.
// Credentials are selected separately for each URL to retain path scoping.
func JenkinsCommand(c config.Config, job core.Job) (string, error) {
	if !NewJenkins(c).Match(job) {
		return "", fmt.Errorf("selected check is not a Jenkins build; use Tab to select a Jenkins check")
	}
	raw, err := jenkinsBuildRoot(job.URL)
	if err != nil {
		return "", err
	}
	requests := job.JenkinsRequests
	if len(requests) == 0 {
		requests = []string{raw + "/api/json"}
		if !core.Terminal(job.Status) {
			requests = append(requests, estimateURL(raw))
		}
		requests = append(requests, raw+"/wfapi/describe")
	}
	var commands []string
	for _, endpoint := range requests {
		if core.BuildURLWarning(endpoint) != "" {
			return "", fmt.Errorf("invalid recorded Jenkins API URL")
		}
		// Explicit --url prevents option injection. No redirects: curl cannot
		// enforce the monitor's origin AND path-prefix redirect restriction.
		args := []string{"curl", "--disable", "--silent", "--show-error", "--fail", "--max-time", strconv.FormatFloat(c.RequestTimeout().Seconds(), 'f', -1, 64), "--header", "Accept: application/json", "--write-out", "\\n", "--url", endpoint}
		command := shellCommand(args)
		if server := jenkinsServer(c, endpoint); server != nil {
			userVar, tokenVar := "gprm_request_user", "gprm_request_token"
			for userVar == server.UserEnv || userVar == server.TokenEnv {
				userVar += "_"
			}
			for tokenVar == server.UserEnv || tokenVar == server.TokenEnv {
				tokenVar += "_"
			}
			user, e := credentialAssignment(userVar, server.UserEnv, server.User)
			if e != nil {
				return "", e
			}
			token, e := credentialAssignment(tokenVar, server.TokenEnv, server.Token)
			if e != nil {
				return "", e
			}
			command = "(\n" + user + token + "set --\nif [ -n \"$" + userVar + "$" + tokenVar + "\" ]; then set -- --user \"$" + userVar + ":$" + tokenVar + "\"; fi\n" + command + " \"$@\"\n)"
		}
		commands = append(commands, command)
	}
	return "(\n" + strings.Join(commands, "\n") + "\n)\n", nil
}
