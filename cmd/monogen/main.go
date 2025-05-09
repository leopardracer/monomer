package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "monogen",
		Short: "monogen scaffolds a Monomer project.",
		Long: "monogen scaffolds a Monomer project. " +
			"The resulting project is compatible with the ignite tool (https://github.com/ignite/cli).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return Generate(cmd.Context(), appDirPath, goModulePath, addressPrefix, monomerPath)
		},
	}

	appDirPath    string
	goModulePath  string
	addressPrefix string
	monomerPath   string
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	rootCmd.Flags().StringVar(&appDirPath, "app-dir-path", "./testapp", "project directory")
	rootCmd.Flags().StringVar(&goModulePath, "gomod-path", "github.com/testapp/testapp", "go module path")
	rootCmd.Flags().StringVar(&addressPrefix, "address-prefix", "cosmos", "address prefix")
	rootCmd.Flags().StringVar(&monomerPath, "monomer-path", "", "local monomer repo path for debugging")

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		cancel()   // cancel is not called on os.Exit, we have to call it manually
		os.Exit(1) //nolint:gocritic // Doesn't recognize that cancel() is called.
	}
}
