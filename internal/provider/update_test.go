package provider

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestUpdateAvailabilityComesFromGitHub(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/api#42", "github.com")
	g.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "viewerCanUpdateBranch") {
			t.Fatal("availability not queried")
		}
		return []byte(`{"data":{"repository":{"pullRequest":{"title":"ABC-1: Fix","state":"OPEN","headRefOid":"abc","baseRefName":"release","viewerCanUpdateBranch":true}}}}`), nil
	})
	p := g.Fetch(context.Background(), ref)
	if p.Error != "" || !p.Details.ViewerCanUpdateBranch || p.Details.BaseBranch != "release" {
		t.Fatalf("%+v", p)
	}
}

func TestUpdateBranchUsesExpectedHeadAndConfiguredCLI(t *testing.T) {
	c := config.Defaults()
	c.Tools.GH = "/tools/custom gh"
	g := New(c)
	ref, _ := core.ParseRef("acme/api#42", "github.example.com")
	p := core.PR{Ref: ref, Head: strings.Repeat("a", 40), State: "OPEN", Fresh: true, Details: core.PRDetails{ViewerCanUpdateBranch: true}}
	calls := 0
	g.Runner = runnerFunc(func(ctx context.Context, path string, args ...string) ([]byte, error) {
		calls++
		if path != c.Tools.GH {
			t.Fatal(path)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("no timeout")
		}
		want := []string{"api", "--hostname", ref.Host, "--method", "PUT", "repos/acme/api/pulls/42/update-branch", "-f", "expected_head_sha=" + p.Head}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("%v", args)
		}
		return []byte(`{"message":"Updating pull request branch."}`), nil
	})
	if err := g.UpdateBranch(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*core.PR){
		func(p *core.PR) { p.Fresh = false }, func(p *core.PR) { p.Error = "offline" },
		func(p *core.PR) { p.State = "MERGED" }, func(p *core.PR) { p.State = "CLOSED" },
		func(p *core.PR) { p.Head = "" }, func(p *core.PR) { p.Details.ViewerCanUpdateBranch = false },
		func(p *core.PR) { p.Ref.Repo = "acme/another" },
	} {
		bad := p
		change(&bad)
		if g.UpdateBranch(context.Background(), bad) == nil {
			t.Fatal("accepted invalid update")
		}
	}
	if calls != 1 {
		t.Fatal("invalid update invoked CLI")
	}
	g.Runner = runnerFunc(func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("HTTP 422") })
	if err := g.UpdateBranch(context.Background(), p); err == nil || !strings.Contains(err.Error(), "branch update failed") {
		t.Fatal(err)
	}
}
