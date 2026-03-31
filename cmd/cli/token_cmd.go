package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// tokenDeprecatedCmd is a hidden backward-compatibility shim.
var tokenDeprecatedCmd = &cobra.Command{
	Use:    "token",
	Short:  "Deprecated: use 'account credential' instead",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("The 'token' command has been replaced by 'account credential'.")
		fmt.Println()
		fmt.Println("  gitbox account credential setup    <account-key>   # store credential")
		fmt.Println("  gitbox account credential verify <account-key>   # verify credential")
		fmt.Println("  gitbox account credential del    <account-key>   # remove credential")
		return nil
	},
}
