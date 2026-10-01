# Regenerate from the repository root with `make demo-gif`.
# Pressed menu items flash pink with black text; no key-history overlay.
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
Type "./bin/gprm --demo --interval 2s"
Enter
Wait+Screen /5 PRs/
Sleep 3s

# Repo -> progress -> Jira: correlate repositories and target branches.
Type "s"
Sleep 2s
Type "s"
Wait+Screen /sort jira/
Sleep 3s

# Focus the first ticket's details, then scroll to the CI URL.
Type "g"
Right
Wait+Screen /DETAILS/
Sleep 3s
PageDown
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
Sleep 3s
Escape
Type "q"
Wait+Screen /OBSERVED RUNS/
Sleep 4s
