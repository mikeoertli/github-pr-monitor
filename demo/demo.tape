# Regenerate from the repository root with `make demo-gif`.
# Pressed menu items flash pink with black text.
# The footer shows u/U for branch updates; do not invoke remote-update actions.
# Entirely offline: no credentials, saved session, browser, or clipboard access.
Output assets/gprm-demo.gif
Set Shell "bash"
Set FontFamily "Menlo"
Set FontSize 18
Set Width 1920
Set Height 660
Set Padding 24
Set Theme "Catppuccin Mocha"
Set Framerate 8
Set TypingSpeed 60ms
Set WaitTimeout 15s
Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env PS1 "$ "
Env HISTFILE "/dev/null"

Hide
Type "unset PROMPT_COMMAND NO_COLOR; clear"
Enter
Show
Type "./bin/gprm --demo --interval 2s --sort jira,target,repo"
Enter
Wait+Screen /5 PRs/
# Opening PR/CI links leads the footer; keep browser actions offline.
Wait+Screen /BROWSE.*Open PR.*Open CI/
Wait+Screen /PRS.*Add/
Sleep 3s

# Cycle repo -> progress -> Jira; start with the configured compound order.
Type "s"
Sleep 2s
Type "s"
Sleep 2s
Type "s"
Wait+Screen /sort jira/
Sleep 3s

# The demo hides support/ in TARGET, while details retain the full branch name.
Down
Right
Wait+Screen /DETAILS/
Wait+Screen /support.release.1/
Wait+Screen /PR OVERVIEW/
Sleep 3s
PageDown
Wait+Screen /CI CHECKS/
Sleep 3s
Left
Sleep 1s

# The same fuzzy filter drives the table and polling.
Type "/"
Sleep 700ms
Type "ABC-42"
Enter
Wait+Screen /2 PRs/
Sleep 3s
Escape
Sleep 1s

# Inspect a failed build, including its relative completion timestamp.
Type "/"
Sleep 700ms
Type "worker"
Enter
Wait+Screen /failed 90m ago/
Sleep 3s
Right
Sleep 3s
Left
Escape

# Pause, review shortcuts, and finish with the monitoring summary.
Type "p"
Sleep 1s
Type "?"
Wait+Screen /CONFIGURATION/
Wait+Screen /LEGEND/
Sleep 5s
Escape
Type "q"
Wait+Screen /OBSERVED RUNS/
Sleep 4s
