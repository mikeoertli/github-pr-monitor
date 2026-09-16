# github-pr-monitor (`gprm`)

A keyboard-driven GitHub PR and build dashboard, styled after [kube-resource-monitor](https://github.com/mikeoertli/kube-resource-monitor). Paste a batch of PR links, follow their CI checks, and leave with a useful monitoring summary.

- Jenkins build numbers, elapsed-time estimates, active pipeline stages, and superseding builds.
- GitHub Actions run numbers, attempts, and active steps. Other CI providers work through GitHub check runs and commit statuses.
- Fuzzy filtering, repository/progress sorting, browser shortcuts, and automatic session restoration.
- Configurable startup and exit behavior; no-check and stale snapshots never count as passing.
- Mixed-provider sessions automatically show a CI column on wide terminals.
- An offline demo that never touches your saved session or calls GitHub/Jenkins.

## Build and install

Requires Go 1.24.2 or later to build, and an installed, authenticated [GitHub CLI](https://cli.github.com/) for live monitoring. Jenkins uses Go's HTTP client; neither curl nor jq is needed.

```sh
gh auth login
make build
./bin/gprm --demo
make install
```

`make install` installs `gprm`, `ghprm`, and `github-pr-monitor` into `~/.local/bin`. Set `PREFIX` to change the destination. Ensure the destination is on your `PATH`. `go install ./cmd/gprm` is also supported, without aliases.

## Shell completions

`make build` generates Bash, Zsh, Fish, and PowerShell scripts in `bin/completions`. You can also generate any script directly:

```sh
gprm completion zsh
gprm completion bash
gprm completion fish
gprm completion powershell
```

Completions cover flags, startup modes, auto-quit modes, sort choices, interval examples, and file paths. All three command names are supported. Generating or requesting completions works offline, without `gh`, and does not read or write your config or session.

**Zsh** (the default macOS shell):

```sh
mkdir -p ~/.local/share/zsh/site-functions
gprm completion zsh > ~/.local/share/zsh/site-functions/_gprm
```

Add the following to `~/.zshrc`, with the `fpath` line **before** your existing `compinit` call or shell-framework initialization. If completion is already initialized by your framework, keep its initialization instead of adding a second `compinit` call.

```zsh
fpath=("$HOME/.local/share/zsh/site-functions" $fpath)
autoload -Uz compinit
compinit
```

Start a new shell. To enable completions just for the current session after `compinit` has run: `source <(gprm completion zsh)`.

**Bash:**

First install and load `bash-completion`. For macOS's bundled Bash 3.2, use `brew install bash-completion`; for Homebrew Bash (4.2 or newer), use `brew install bash-completion@2`. Add this before the gprm source line in your Bash startup file:

```bash
source "$(brew --prefix)/etc/profile.d/bash_completion.sh"
```

On Linux, install your distribution's `bash-completion` package and load its initialization script (commonly `/usr/share/bash-completion/bash_completion`) if your shell does not already do so. This dependency supplies the word parsing and file completion helpers used by the generated Bash script.

```sh
mkdir -p ~/.local/share/bash-completion/completions
gprm completion bash > ~/.local/share/bash-completion/completions/gprm
```

Add `source "$HOME/.local/share/bash-completion/completions/gprm"` to `~/.bashrc`. On macOS, ensure your `~/.bash_profile` loads `~/.bashrc`, or add the source line there instead. The script registers all three command names. For the current session only: `source <(gprm completion bash)`.

**Fish:**

```fish
mkdir -p ~/.config/fish/completions
gprm completion fish > ~/.config/fish/completions/gprm.fish
ln -sf gprm.fish ~/.config/fish/completions/ghprm.fish
ln -sf gprm.fish ~/.config/fish/completions/github-pr-monitor.fish
```

Use your Fish configuration directory instead if customized. The alias files allow Fish to load completions even when an alias is the first command you complete. For the current session only: `gprm completion fish | source`.

**PowerShell:** add this line to your PowerShell profile (`$PROFILE`), or run it in the current session:

```powershell
gprm completion powershell | Out-String | Invoke-Expression
```

Alternatively, save `gprm completion powershell` to a `.ps1` file and dot-source that file from your profile.

**Install generated files together:** `make install-completions` installs all four scripts under `$(PREFIX)/share` (default `~/.local/share`), including Bash/Fish alias links. This is separate from `make install` and does not edit shell startup files. For Zsh/Bash, use the setup above; Fish must search `$(PREFIX)/share/fish/vendor_completions.d` in its completion path (or use the per-user installation above). The PowerShell script is installed at `$(PREFIX)/share/gprm/completions/gprm.ps1`.

## Start monitoring

```sh
gprm                                         # restore the last session
gprm --startup clipboard                     # start with PR links in the clipboard
gprm --startup empty                         # start with an empty dashboard
gprm --startup auto-discover                  # start with your open PRs
gprm --startup empty acme/api#42 acme/web#87   # explicit PRs only
gprm --auto-quit all-passing --interval 10s
gprm --auto-quit builds-finished acme/api#42
gprm --auto-quit never
gprm --demo                                  # animated, fully offline
gprm --demo --once                           # printable demo snapshot
gprm --once --startup clipboard              # one live refresh and summary
```

Place flags before PR references. Positional PRs are added to the selected startup mode. URLs can include `/files`, `/checks`, query strings, and fragments; they are normalized to the PR. `owner/repo#123` uses `github_host` from settings. Clipboard import extracts links from prose and Markdown and deduplicates them. `clipboard` startup reads once; press `v` to import again.

Discovery uses your authenticated GitHub account and searches for authored, open PRs. It follows pages up to `discovery_limit` (100 by default, maximum 1000) and reports when the limit is reached. It runs on startup or when you press `d`, not on every refresh.

## Configuration

On the first live run, gprm creates `~/.config/gprm/gprm_config.toml` with documented defaults and permissions `0600`. `$XDG_CONFIG_HOME/gprm/gprm_config.toml` is used when set. Create it ahead of time with:

```sh
gprm --init-config
```

This refuses to overwrite an existing file. `--config /path/to/gprm_config.toml` selects another file. Unknown settings and invalid modes/durations produce an error.

If you previously used `~/.config/gprm/config.toml`, rename it to `gprm_config.toml` in the same directory before the next live run, or keep using it explicitly with `--config ~/.config/gprm/config.toml`. Existing files are not automatically renamed or overwritten. The default template is embedded from `internal/config/defaults.toml`, which also supplies runtime defaults for omitted settings.

```toml
startup = "restore"          # restore | clipboard | empty | auto-discover
auto_quit = "all-closed"    # never | builds-finished | all-passing | all-closed
interval = "5s"
request_timeout = "20s"
sort = "repo"              # repo | progress
descending = false
ci_column = "auto"         # auto | always | never
github_host = "github.com"
discovery_limit = 100

[tools]
gh = "gh"                 # executable name or absolute path
clipboard = ""            # automatic platform default
clipboard_args = []
open = ""                 # automatic platform default
open_args = []

[[jenkins]]
url = "https://ci.example.com/jenkins"
user = ""
token = ""
user_env = "JENKINS_USER"
token_env = "JENKINS_API_TOKEN"
```

Repeat `[[jenkins]]` for more servers. The environment variables named by `user_env` and `token_env` override inline values when nonempty. Credentials are sent only to the configured origin and path prefix. Cross-origin redirects are refused. Use the actual Jenkins server root, including any context path such as `/jenkins`.

Paths and arguments are separate: `clipboard = "/path with spaces/helper"` works; `clipboard = "helper --flag"` does not. Commands are executed directly, without a shell. Defaults are `pbpaste`/`open` on macOS, `wl-paste` or `xclip`/`xsel` and `xdg-open` on Linux, and PowerShell clipboard/rundll32 on Windows. Windows integrations are implemented but have not been tested on Windows. Manual paste into the `a` input works when no clipboard helper is available.

CLI overrides: `--startup`, `--auto-quit`, `--interval`, `--sort`, and `--gh`. Overrides affect the current run and do not rewrite your settings. Demo mode uses built-in defaults and CLI overrides; it ignores the config file.

## Exit modes

| Mode | Exit when every monitored PR… |
| --- | --- |
| `never` | Waits for you to quit. |
| `builds-finished` | Has at least one check and all currently reported checks have reached a terminal result, including failure/cancellation. |
| `all-passing` | Has at least one check and all currently reported checks are successful, neutral, or skipped. |
| `all-closed` (default) | Is merged or closed, independently of CI results or running jobs. |

The full monitored list is used, even while filtered. An empty list never auto-quits. Restored state must be refreshed before it can cause an exit. A GitHub polling error blocks all automatic exits; a Jenkins build polling error blocks the build-related exit modes. Missing optional stage/step details do not override an authoritative build/check result. Pausing prevents auto-quit, and adding/importing PRs postpones it until a subsequent refresh.

“All passing” covers all reported checks, not just branch-protection-required checks. No checks means waiting, even for a closed PR in a build-related mode. Polling cannot predict checks that a provider has not registered yet or reruns started after the last snapshot. Once a build-related mode exits, it cannot observe future reruns.

## Keys

| Key | Action |
| --- | --- |
| `v` / `Ctrl+V` | Add all PR links from the clipboard |
| `a` | Type or paste one or more PR URLs or `owner/repo#123` references |
| `d` | Add your open PRs |
| `↑` / `k`, `↓` / `j` | Select a PR |
| `PgUp`, `PgDn`, `g`, `G` | Page or jump to the first/last PR |
| `Tab` / `Enter`, `Shift+Tab` | Next/previous check in the detail pane |
| `/`, `Esc` | Fuzzy filter; clear filter |
| `s`, `r` | Switch repository/progress sort; reverse direction |
| `p`, `R` | Pause/resume; refresh immediately |
| `o`, `b` | Open the selected PR or selected check's build URL |
| `x` | Remove a PR from monitoring; retain it in this run's summary |
| `?` | Show help |
| `Q` / `q` / `Ctrl+C` | Quit and print the summary |

The table shows the first active check (or the first check when all are finished). The detail pane lets you inspect and open every check. At narrow terminal widths the CI/phase columns are omitted; those details remain in the selected-check pane. `NO_COLOR` disables colors.

## CI behavior and progress

GitHub is queried through `gh api graphql` with pagination for all check contexts on the current PR head. A changed head during pagination invalidates that snapshot. Polling runs asynchronously with at most four PRs being fetched concurrently, keeping the interface responsive. The default interval is five seconds; an in-flight refresh is never overlapped by a second refresh of the same set.

**Jenkins:** the CI URL reported by GitHub is used directly. A direct Jenkins build URL such as `https://ci.example.com/job/api/job/PR-42/17/` enables `/api/json` and optional `/wfapi/describe` requests. There is no legacy/Blue Ocean conversion. Non-build links fall back to the GitHub-reported status with an explanatory detail message. A direct build URL, configured server match, or Jenkins check/provider name identifies Jenkins.

Time estimates prefer the same job's last successful duration, falling back to Jenkins's `estimatedDuration`. Estimates are marked `~`, cap at 99% while running, and display overruns in the phase. Jobs without timing information show unknown progress. Aborted/not-built runs may follow `nextBuild` within the same job, with a bounded chain. Pipeline stages require the [Pipeline REST API plugin](https://plugins.jenkins.io/pipeline-rest-api/); its absence does not prevent build monitoring.

**GitHub Actions:** active job steps and completed-step progress come from the Actions API; build numbers show the workflow run number and rerun attempt (for example, `42.2`). Workflow names distinguish identically named jobs. If job details are unavailable, GitHub's check status remains available.

**Other providers:** provider name, check name, status, and build link come from GitHub. Running percentage is unknown unless the provider exposes supported detail data. Finished checks show 100%, including failed checks: the bar indicates completion, while the status indicates outcome. Generic providers do not promise stage names or numeric build numbers. PR progress averages check progress only when every check has a known value; otherwise it shows `—`.

## Saved sessions and exit summary

Sessions are written atomically, with permissions `0600`, to `~/.local/state/gprm/session.json` (or `$XDG_STATE_HOME/gprm/session.json`). `--state /path/to/session.json` allows separate named sessions. Use different state paths for concurrent monitors. A corrupt saved session produces an error instead of being overwritten during restore.

`restore` keeps PRs, observed run history, and accumulated monitoring time. Other startup modes start a new session and replace the saved session. Removed PRs appear in the current exit summary but are not restored. The app checkpoints periodically and after changes, then saves again on normal quit, Ctrl+C, or SIGTERM.

The summary includes each PR and CI job, distinct **observed** run counts, accumulated time monitored, and final PR/build status. Repeated polls of the same run do not increase the count. Counters persist through restoration and do not add time while the app is closed. Generic status providers that reuse a URL and expose no run identity cannot reliably distinguish reruns; counts are therefore observational, not a complete historical audit.

A merge observed while monitoring may be labeled **merged with failing checks**, **merged with pending checks**, or both. The label is retained even if CI later finishes. This describes the checks at the first observed merge transition, not proof of an administrative bypass. A PR already merged when first added is simply `merged`; merges missed between polls cannot be reconstructed exactly.

## Development

```sh
make test                  # includes the race detector
make check                 # formatting, go vet, and tests
make build
```

`VERSION` is the sole source of the application version. It is embedded at compile time for Make builds and direct `go build`, `go install`, and `go run` commands. To change the version, edit `VERSION` and rebuild; `gprm --version` reports the embedded value. No version flag, Make variable, or linker override is needed.

Tests cover startup/config overrides, paginated GitHub results, changed PR heads, CI fallbacks, Jenkins credential scoping/redirects, superseded builds, estimates, run counting, merge observations, auto-quit semantics, session restoration, and terminal-size handling. They use fixtures and a fake HTTP transport; live Jenkins credentials are not needed.
