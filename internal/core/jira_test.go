package core

import (
	"reflect"
	"testing"
)

func TestJiraID(t *testing.T) {
	for _, tc := range []struct {
		title    string
		prefixes []string
		want     string
	}{
		{"abc-123: Improve caching", []string{"ABC"}, "ABC-123"},
		{"ABC-123 Improve caching", []string{"abc", "OPS"}, "ABC-123"},
		{"ops-7", nil, "OPS-7"},
		{"TEAM_2-99 / task", nil, "TEAM_2-99"},
		{"OTHER-12 task", []string{"ABC"}, ""},
		{"Fix ABC-123", nil, ""},
		{"[ABC-123] Fix", nil, ""},
		{"ABC-123suffix", nil, ""},
		{"ABC-0 task", nil, ""},
		{"ABC- task", nil, ""},
		{"123-42 task", nil, ""},
	} {
		if got := JiraID(tc.title, tc.prefixes); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestJiraSortGroupsTargetBranchesAndRepositories(t *testing.T) {
	prs := []PR{
		{Title: "No ticket", Ref: Ref{Repo: "acme/a", Number: 1}},
		{Title: "ABC-9: fix", Ref: Ref{Repo: "acme/b", Number: 2}, Details: PRDetails{BaseBranch: "main"}},
		{Title: "abc-9: backport", Ref: Ref{Repo: "acme/a", Number: 3}, Details: PRDetails{BaseBranch: "release", Branch: "z"}},
		{Title: "ABC-9: fix", Ref: Ref{Repo: "acme/a", Number: 4}, Details: PRDetails{BaseBranch: "main"}},
		{Title: "OPS-1: fix", Ref: Ref{Repo: "acme/a", Number: 5}},
		{Title: "ABC-9: backport", Ref: Ref{Repo: "acme/a", Number: 6}, Details: PRDetails{BaseBranch: "release", Branch: "a"}},
	}
	if got := Sorted(prs, "", "jira", false); !reflect.DeepEqual(got, []int{3, 1, 5, 2, 4, 0}) {
		t.Fatal(got)
	}
	if got := Sorted(prs, "", "jira", true); !reflect.DeepEqual(got, []int{4, 2, 5, 1, 3, 0}) {
		t.Fatal(got)
	}
	if got := Sorted(prs, "", "jira", false, "OPS"); got[0] != 4 {
		t.Fatal(got)
	}
	if got := Sorted(prs, "ABC-9 release", "jira", false); !reflect.DeepEqual(got, []int{5, 2}) {
		t.Fatal(got)
	}
}

func TestCompoundSortAndUnticketedFallback(t *testing.T) {
	prs := []PR{
		{Title: "None", Ref: Ref{Repo: "acme/z", Number: 1}, Details: PRDetails{BaseBranch: "main"}},
		{Title: "ABC-2 work", Ref: Ref{Repo: "acme/a", Number: 2}, Details: PRDetails{BaseBranch: "release"}},
		{Title: "None", Ref: Ref{Repo: "acme/a", Number: 3}, Details: PRDetails{BaseBranch: "release"}},
		{Title: "ABC-2 work", Ref: Ref{Repo: "acme/z", Number: 4}, Details: PRDetails{BaseBranch: "main"}},
		{Title: "None", Ref: Ref{Repo: "acme/b", Number: 5}, Details: PRDetails{BaseBranch: "main"}},
	}
	for _, tc := range []struct {
		by   string
		desc bool
		want []int
	}{
		{"jira,target,repo", false, []int{3, 1, 4, 0, 2}},
		{"jira", false, []int{3, 1, 4, 0, 2}},
		{"jira,target,repo", true, []int{1, 3, 2, 0, 4}},
		{"jira,repo,target", false, []int{1, 3, 2, 4, 0}},
		{"target,repo", false, []int{4, 0, 3, 1, 2}},
	} {
		if got := Sorted(prs, "", tc.by, tc.desc); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s desc=%v: %v", tc.by, tc.desc, got)
		}
	}
	// Without any recognized IDs, Jira sorting reduces to target/repository sorting.
	if got := Sorted(prs, "", "jira,target,repo", false, "OTHER"); !reflect.DeepEqual(got, []int{4, 0, 3, 1, 2}) {
		t.Fatal(got)
	}
}
