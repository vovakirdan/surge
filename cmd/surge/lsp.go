package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"surge/internal/lsp"
)

var lspCmd = &cobra.Command{
	Use:          "lsp",
	Short:        "Run the Surge language server over stdio",
	SilenceUsage: true,
	RunE:         runLSP,
}

func runLSP(cmd *cobra.Command, _ []string) error {
	server := lsp.NewServer(os.Stdin, os.Stdout, lsp.ServerOptions{})
	if err := server.Run(cmd.Context()); err != nil {
		if errors.Is(err, lsp.ErrExit) {
			return nil
		}
		if errors.Is(err, lsp.ErrExitWithoutShutdown) {
			return fmt.Errorf("lsp exit without shutdown")
		}
		return err
	}
	return nil
}
