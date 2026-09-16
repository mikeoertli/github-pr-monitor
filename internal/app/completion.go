package app

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Reuse the live CLI's flag definitions so generated completions stay in sync.
// The command tree is used only for script generation and completion requests;
// no completion can start monitoring, read credentials, or call a remote API.
func runCompletion(flags *pflag.FlagSet, args []string, out, stderr io.Writer) error {
	root := &cobra.Command{
		Use: "gprm", Short: "GitHub PR and build tracker",
		SilenceUsage: true, SilenceErrors: true,
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
	root.SetOut(out)
	root.SetErr(stderr)
	root.SetArgs(args)
	root.CompletionOptions.DisableDefaultCmd = true
	// The monitoring CLI has --help, but no "help" subcommand to suggest.
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true})
	root.Flags().AddFlagSet(flags)
	values := map[string][]string{
		"startup":   {"restore", "clipboard", "empty", "auto-discover"},
		"auto-quit": {"never", "builds-finished", "all-passing", "all-closed"},
		"sort":      {"repo", "progress"},
		"interval":  {"1s", "5s", "10s", "30s", "1m"},
	}
	for name, choices := range values {
		if err := root.RegisterFlagCompletionFunc(name, cobra.FixedCompletions(choices, cobra.ShellCompDirectiveNoFileComp)); err != nil {
			return err
		}
	}
	for _, name := range []string{"config", "state", "gh"} {
		if err := root.MarkFlagFilename(name); err != nil {
			return err
		}
	}
	root.AddCommand(&cobra.Command{
		Use:       "completion bash|zsh|fish|powershell",
		Short:     "Generate shell completions for gprm and both aliases",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateCompletion(cmd.Root(), args[0], cmd.OutOrStdout())
		},
	})
	return root.Execute()
}

func generateCompletion(root *cobra.Command, shell string, out io.Writer) error {
	var b bytes.Buffer
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(&b, true)
	case "zsh":
		err = root.GenZshCompletion(&b)
	case "fish":
		err = root.GenFishCompletion(&b, true)
	case "powershell":
		err = root.GenPowerShellCompletionWithDesc(&b)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
	if err != nil {
		return err
	}
	script := b.String()
	switch shell {
	case "bash":
		script = strings.ReplaceAll(script, "__start_gprm gprm\n", "__start_gprm gprm ghprm github-pr-monitor\n")
	case "zsh":
		script = strings.Replace(script, "#compdef gprm\n", "#compdef gprm ghprm github-pr-monitor\n", 1)
		script = strings.Replace(script, "compdef _gprm gprm\n", "compdef _gprm gprm ghprm github-pr-monitor\n", 1)
	case "fish":
		script += "\ncomplete -c ghprm -w gprm\ncomplete -c github-pr-monitor -w gprm\n"
	case "powershell":
		script = strings.Replace(script, "-CommandName 'gprm'", "-CommandName 'gprm', 'ghprm', 'github-pr-monitor'", 1)
	}
	_, err = io.WriteString(out, script)
	return err
}
