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

var plainShellArg = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellArg(s string) string {
	if plainShellArg.MatchString(s) {
		return s
	}
	return shellQuote(s)
}

func shellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, s := range args {
		quoted[i] = shellArg(s)
	}
	return strings.Join(quoted, " ")
}

const prViewFields = "additions,assignees,author,autoMergeRequest,baseRefName,baseRefOid,body,changedFiles,closed,closedAt,closingIssuesReferences,comments,commits,createdAt,deletions,files,fullDatabaseId,headRefName,headRefOid,headRepository,headRepositoryOwner,id,isCrossRepository,isDraft,labels,latestReviews,maintainerCanModify,mergeCommit,mergeStateStatus,mergeable,mergedAt,mergedBy,milestone,number,potentialMergeCommit,projectCards,projectItems,reactionGroups,reviewDecision,reviewRequests,reviews,state,statusCheckRollup,title,updatedAt,url"

// GitHubCommand provides a readable, single request for the selected PR.
func GitHubCommand(c config.Config, p core.PR) string {
	return shellCommand([]string{c.Tools.GH, "pr", "view", p.Ref.URL, "--json", prViewFields}) + "\n"
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

// credentialPart keeps environment values symbolic, with inline fallbacks.
func credentialPart(name, fallback string) (string, error) {
	if name == "" {
		return shellQuote(fallback), nil
	}
	if !shellEnvName.MatchString(name) {
		return "", fmt.Errorf("configured Jenkins environment variable name is not valid for a shell command")
	}
	if fallback == "" {
		return "\"$" + name + "\"", nil
	}
	// A separately quoted default inside the expansion preserves arbitrary
	// credentials without interpreting dollars, backticks, or closing braces.
	literal := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`").Replace(fallback)
	return "\"${" + name + ":-\"" + literal + "\"}\"", nil
}

// JenkinsCommand retrieves the selected build's JSON in one readable command.
func JenkinsCommand(c config.Config, job core.Job) (string, error) {
	if !NewJenkins(c).Match(job) {
		return "", fmt.Errorf("selected check is not a Jenkins build; use Tab to select a Jenkins check")
	}
	raw, err := jenkinsBuildRoot(job.URL)
	if err != nil {
		return "", err
	}
	endpoint := raw + "/api/json"
	command := "curl -s"
	if server := jenkinsServer(c, endpoint); server != nil {
		if server.User != "" || server.Token != "" || server.UserEnv != "" || server.TokenEnv != "" {
			user, err := credentialPart(server.UserEnv, server.User)
			if err != nil {
				return "", err
			}
			token, err := credentialPart(server.TokenEnv, server.Token)
			if err != nil {
				return "", err
			}
			auth := user + ":" + token
			if server.UserEnv != "" && server.TokenEnv != "" && server.User == "" && server.Token == "" {
				auth = "\"$" + server.UserEnv + ":$" + server.TokenEnv + "\""
			} else if server.UserEnv == "" && server.TokenEnv == "" {
				auth = shellQuote(server.User + ":" + server.Token)
			}
			command += " -u " + auth
		}
	}
	return command + " " + shellArg(endpoint) + "\n", nil
}
