package core

import (
	"regexp"
	"strings"
)

var jiraPrefix = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*)-([1-9][0-9]*)(?:$|[^A-Za-z0-9_])`)

// JiraID recognizes only leading ticket IDs, with an optional project allowlist.
func JiraID(title string, prefixes []string) string {
	match := jiraPrefix.FindStringSubmatch(strings.TrimSpace(title))
	if match == nil {
		return ""
	}
	allowed := len(prefixes) == 0
	for _, prefix := range prefixes {
		if strings.EqualFold(prefix, match[1]) {
			allowed = true
			break
		}
	}
	if !allowed {
		return ""
	}
	return strings.ToUpper(match[1]) + "-" + match[2]
}
