# Changelog

## 1.1.0 — Work in progress

- Organize links under BROWSE (`g` PR, `c` CI, `J` Jira) and commands under COPY (`G` gh, `C` Jenkins curl, `K` Jira URL); remove PR/table JSON clipboard exports. Keep discovery and manual Add under PRS. Use Home/End for first/last navigation.
- Support saved and CLI compound sort orders such as `jira,target,repo`. Unticketed PRs follow Jira groups, ordered by the remaining keys; `jira` uses target, repository, and source branch order.
- Make help easier to scan with styled sections and keys, a colored status legend, two columns in wide windows, and scrolling in smaller windows.
- Make details easier to scan with section headings, bold labels, status colors, emphasized target branches, and underlined URLs; preserve plain-text readability with `--no-color`.
- Add `target_branch_ignored_prefixes` to shorten TARGET labels while preserving full branch names in details and actions.
- Add `--print-config` for all documented settings/defaults and `--edit-config` (`-e`) to create or edit the selected config in `$EDITOR`, then validate it.

## 1.0.0 — 2026-10-01

- Show the target branch in the table and detect leading Jira ticket IDs case-insensitively, with configurable project prefixes and base URL.
- Add Jira URL open/copy shortcuts and grouping by ticket, repository, target branch, and source branch (`--sort jira`, also included in completions).
- Indicate available branch updates and apply them directly from the selected row with `u` (confirm) or `U` (without confirmation), protection against changed heads, and a refresh after GitHub accepts the request. Prevent auto-quit while an update is pending.
- Show relative status ages from provider event times, with explicitly marked first-observed fallbacks that survive refreshes and session restores.
- Briefly highlight pressed menu items with a bright pink background and black text.
- Add a README demo GIF and a reproducible VHS tape with `make demo-gif`.

## 0.5.0 — 2026-09-30

- Discover PRs on launch in the default restore mode, preserving saved PRs, history, and dismissals while adding new results without duplicates.

## 0.4.0 — 2026-09-21

- Show merge readiness and reasons for PRs without CI checks, including conflicts, draft/review requirements, and unknown states.
- Simplify copied commands to one `gh pr view --json` command or one Jenkins build `curl` command.

- Add `-f` / `--filter` to select displayed and polled PRs using fuzzy matching, without restricting discovery.
- Share the filter with `/` editing, apply it to `--once`, summaries, and auto-quit, and retain excluded PRs in the saved session.

## 0.3.2 — 2026-09-21

- Resolve Jenkins report links to their numbered build, including coverage paths and test-page query parameters.
- Deduplicate reports with their parent build for polling/progress, preferring the build overview for GitHub fallback status.

## 0.3.1 — 2026-09-17

- Close focused details and return to the table with a single Left or Esc press.

## 0.3.0 — 2026-09-17

- Emphasize footer headings and shortcut keys with bold/color while preserving no-color mode.
- Add focused details navigation with arrow keys, page scrolling, line ranges, and overflow indicators.

- Retain completed PRs for 24 hours by default, with configurable duration/forever retention and a CLI override.
- Remember dismissals, retain completion history on restore/auto-discovery, and discover recently closed PRs.
- Show expiry/dismiss controls while preserving existing auto-quit behavior.

## 0.2.0 — 2026-09-17

- Accept Jenkins `/display/redirect` build links, populate build numbers before fetching, and normalize copied curl endpoints.
- Respect explicit GitHub merge/closure facts, show lifecycle counts, and hide mergeability for closed PRs.
- Distinguish source update times, successful data fetches, and failed attempts; reject older PR snapshots.

- Add runnable gh and Jenkins curl request copying, preserving API sources and credential environment references.

- Group footer shortcuts and size table columns to the terminal, with branch/title columns on wider screens.
- Add expandable PR metadata, full wrapped PR/CI URLs, and check selection with detail scrolling.
- Surface missing/invalid CI URLs and failed detail lookups with a left-hand warning icon and explanation.
- Add selected-PR and filtered-table JSON clipboard exports, freshness timestamps, and configurable clipboard writers.

- Use standard long options and short aliases, including options after PR references.
- Add `--no-color` and configurable styling, with progress and overdue colors.

## 0.1.0 — 2026-09-16

- Name the default settings file `gprm_config.toml` and embed the documented TOML defaults.
- Embed the application version from `VERSION` for every build path.
- Include formatting, static analysis, and tests in `make check`.

- initial version of the GitHub PR monitor TUI utility
