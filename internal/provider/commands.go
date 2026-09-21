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
	invalid := fmt.Errorf("selected check has no numbered Jenkins build URL; copy the gh command for its GitHub status")
	u, err := url.Parse(raw)
	if err != nil || core.BuildURLWarning(raw) != "" {
		return "", invalid
	}
	// Split the escaped path so a multibranch job name containing %2F stays
	// one segment. Reports, query parameters, and fragments are not API roots.
	parts := strings.Split(u.EscapedPath(), "/")
	firstJob := -1
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil || decoded == "." || decoded == ".." {
			return "", invalid
		}
		if firstJob < 0 && part == "job" {
			firstJob = i
		}
	}
	if firstJob < 0 {
		return "", invalid
	}
	i := firstJob
	for i+1 < len(parts) && parts[i] == "job" && parts[i+1] != "" {
		i += 2
	}
	if i >= len(parts) || parts[i] == "" || strings.Trim(parts[i], "0123456789") != "" {
		return "", invalid
	}
	number, err := strconv.Atoi(parts[i])
	if err != nil || number < 1 {
		return "", invalid
	}
	// Only the /job/<name> hierarchy determines the build number. Numbers in
	// report paths must never be mistaken for another run.
	u.RawPath = strings.Join(parts[:i+1], "/")
	u.Path, _ = url.PathUnescape(u.RawPath)
	u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = "", "", "", false
	return u.String(), nil
}

// Prefer a build overview over a report's GitHub status when Jenkins is offline.
func jenkinsOverview(raw, root string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Query().Get("page") != "" {
		return false
	}
	u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = "", "", "", false
	base := strings.TrimRight(u.String(), "/")
	return base == root || base == root+"/display/redirect"
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
