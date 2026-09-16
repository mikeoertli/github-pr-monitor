package main

import (
	"fmt"
	"os"

	"github.com/mikeoertli/github-pr-monitor/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gprm:", err)
		os.Exit(1)
	}
}
