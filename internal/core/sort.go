package core

import (
	"fmt"
	"strings"
)

// SortKeys parses the configured order. Jira must lead so ticket groups stay contiguous.
func SortKeys(spec string) ([]string, error) {
	if strings.TrimSpace(spec) == "jira" {
		spec = "jira,target,repo,branch"
	}
	keys := strings.Split(spec, ",")
	seen := map[string]bool{}
	for i, key := range keys {
		key = strings.TrimSpace(key)
		switch key {
		case "jira", "target", "repo", "branch", "progress":
		default:
			return nil, fmt.Errorf("sort keys must be jira, target, repo, branch, or progress (comma-separated)")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate sort key: %s", key)
		}
		if key == "jira" && i != 0 {
			return nil, fmt.Errorf("jira must be the first sort key")
		}
		keys[i], seen[key] = key, true
	}
	return keys, nil
}

func GroupsByJira(spec string) bool {
	keys, err := SortKeys(spec)
	return err == nil && keys[0] == "jira"
}
