# Changelog

## 0.3.1 — in progress

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
