package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"charm.land/fang/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/nilox94/tscatalog/internal/catalog"
	"github.com/nilox94/tscatalog/internal/lifecycle"
	"github.com/nilox94/tscatalog/internal/tsapi"
	"github.com/nilox94/tscatalog/internal/tui"
)

// version is set with -ldflags -X main.version=...
var version = "dev"

const longHelp = `Browse the catalog and restart local processes.

Published services that are down or unhealthy are listed first.
r restarts the selected service. R restarts every visible service
whose process needs it. / filters by name.

Restart starts a dead process. If the port is open, it stops and
starts the process only when this catalog launched it. requires
start first and are not published. A published restart republishes
VIP, Serve, and advertise. cat9s does not write the ACL.
A command that fails opens the error along the bottom; esc closes it.

Catalog file (when -f is omitted): TSCATALOG_CATALOG, then
$XDG_CONFIG_HOME/tscatalog/catalog.yaml, then ./catalog.yaml.`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__reap" {
		if err := lifecycle.Reap(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := fang.Execute(context.Background(), newCmd(), fang.WithVersion(version)); err != nil {
		os.Exit(1)
	}
}

func newCmd() *cobra.Command {
	var file string
	var cat *catalog.File
	cmd := &cobra.Command{
		Use:           "cat9s",
		Short:         "Browse and restart catalog services",
		Long:          longHelp,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			catPath, err := catalog.ResolvePath(file)
			if err != nil {
				return err
			}
			if err := tsapi.LoadDotEnv(filepath.Join(filepath.Dir(catPath), ".env")); err != nil {
				return err
			}
			cat, err = catalog.Load(catPath)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !term.IsTerminal(os.Stdout.Fd()) {
				return fmt.Errorf("cat9s needs a terminal")
			}
			return tui.Run(cmd.Context(), cat)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "catalog file (default: auto-discover)")
	return cmd
}
