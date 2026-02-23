package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:           "ledger",
	Short:         "Ledger CLI",
	Long:          `ledger is the command-line interface for the Spot Canvas ledger service.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("ledger-url", "", "Ledger service URL (overrides config/env)")
	rootCmd.PersistentFlags().Bool("json", false, "Output as JSON")

	rootCmd.Version = version
}

func initConfig() {
	loadConfig()
	_ = viper.BindPFlag("ledger_url", rootCmd.PersistentFlags().Lookup("ledger-url"))
}
