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

func TestJiraSortGroupsRepositoriesAndTargetBranches(t *testing.T) {
	prs := []PR{
		{Title: "No ticket", Ref: Ref{Repo: "acme/a", Number: 1}},
		{Title: "ABC-9: fix", Ref: Ref{Repo: "acme/b", Number: 2}, Details: PRDetails{BaseBranch: "main"}},
		{Title: "abc-9: backport", Ref: Ref{Repo: "acme/a", Number: 3}, Details: PRDetails{BaseBranch: "release", Branch: "z"}},
		{Title: "ABC-9: fix", Ref: Ref{Repo: "acme/a", Number: 4}, Details: PRDetails{BaseBranch: "main"}},
		{Title: "OPS-1: fix", Ref: Ref{Repo: "acme/a", Number: 5}},
		{Title: "ABC-9: backport", Ref: Ref{Repo: "acme/a", Number: 6}, Details: PRDetails{BaseBranch: "release", Branch: "a"}},
	}
	if got := Sorted(prs, "", "jira", false); !reflect.DeepEqual(got, []int{3, 5, 2, 1, 4, 0}) {
		t.Fatal(got)
	}
	if got := Sorted(prs, "", "jira", true); !reflect.DeepEqual(got, []int{4, 1, 2, 5, 3, 0}) {
		t.Fatal(got)
	}
	if got := Sorted(prs, "", "jira", false, "OPS"); got[0] != 4 {
		t.Fatal(got)
	}
	if got := Sorted(prs, "ABC-9 release", "jira", false); !reflect.DeepEqual(got, []int{5, 2}) {
		t.Fatal(got)
	}
}
