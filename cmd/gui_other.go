//go:build !windows

package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "gui",
		Short: "Start the native GUI (Windows only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("the native GUI is only available on Windows")
		},
	})
}
