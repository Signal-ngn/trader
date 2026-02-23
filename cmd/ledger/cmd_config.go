package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage local CLI configuration (~/.config/ledger/config.yaml)",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show all resolved config values and their sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		type row struct {
			key    string
			value  string
			source string
		}

		// Collect display rows
		var rows [][]string

		// ledger_url
		rows = append(rows, []string{
			"ledger_url",
			viper.GetString("ledger_url"),
			configSource("ledger_url"),
		})

		// api_key — resolved across three sources
		apiKey, src, _ := resolveAPIKey()
		rows = append(rows, []string{
			"api_key",
			maskValue(apiKey),
			src,
		})

		// tenant_id
		tid := viper.GetString("tenant_id")
		tidSrc := configSource("tenant_id")
		rows = append(rows, []string{"tenant_id", tid, tidSrc})

		fmt.Printf("%-20s %-55s %s\n", "KEY", "VALUE", "SOURCE")
		fmt.Printf("%-20s %-55s %s\n",
			strings.Repeat("-", 20),
			strings.Repeat("-", 55),
			strings.Repeat("-", 10),
		)
		for _, r := range rows {
			fmt.Printf("%-20s %-55s %s\n", r[0], r[1], r[2])
		}
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value in ~/.config/ledger/config.yaml",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		if !isValidKey(key) {
			return fmt.Errorf("unknown config key %q; valid keys: %s", key, strings.Join(validConfigKeys, ", "))
		}
		if err := writeConfigValue(key, value); err != nil {
			return err
		}
		fmt.Printf("Set %s = %s\n", key, value)
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a resolved config value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		if !isValidKey(key) {
			return fmt.Errorf("unknown config key %q; valid keys: %s", key, strings.Join(validConfigKeys, ", "))
		}
		if key == "api_key" {
			apiKey, _, _ := resolveAPIKey()
			fmt.Println(apiKey)
			return nil
		}
		fmt.Println(viper.GetString(key))
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd, configSetCmd, configGetCmd)
	rootCmd.AddCommand(configCmd)
}
