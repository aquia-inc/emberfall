/*
Copyright © 2024 Aquia, Inc.
www.aquia.us
*/
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aquia-inc/emberfall/internal/engine"
	"github.com/spf13/cobra"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:     "emberfall",
	Short:   "Declarative API Testing",
	Version: "0.3.2",
	Run: func(cmd *cobra.Command, args []string) {
		configPath = strings.TrimSpace(configPath)
		conf, err := engine.LoadConfig(configPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if !engine.Run(conf) {
			os.Exit(2)
		}
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "-", "Path to config file. - to read from stdin.")
}
