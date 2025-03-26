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

var (
	configPath string
	include    string
	exclude    string
)

var rootCmd = &cobra.Command{
	Use:   "emberfall",
	Short: "Declarative API Testing",
	Long: `Declarative API testing for much productivity and great good.

Define tests in YAML with the following schema:

tests:
- id: string # required only for include/exclude and referencing
  url: string
  method: string # a supported HTTP method such as GET, POST, PUT, DELETE, etc...
  follow: bool # optional, whether to follow redirects or not, defaults to false
  headers: object # optional, sets headers to be sent with the request
    # arbitrary key:value pairs
  body: object # optional
    text: string # to send as content-type text/plain
    json: object # to send as content-type application/json
      # arbitrary key:value pairs
  expect:
    status: int # a supported HTTP status code such as 200,201,301,400,404, etc...
    body: object # optional
      text: string # to compare to the response body as a text string
      json: object # to compare to the response body as a json object
        # arbitrary key:value pairs
    headers: object # optional, headers expected to be present in the response
      # key:value pairs 

When using --include or --exclude, the value provided to each flag will be 
compiled as a Go-compatible regular expression and used to match against the id 
field for each test. To define multiple tests use the pipe symbol (|), for example
to include foo, bar, and baz use -i 'foo|bar|baz'. To include all tests 
that start with 'ba' us -i '$ba'



When passing both --include and --exclude flags, --exclude will be applied first.
	`,
	Version: "0.3.2",
	Run: func(cmd *cobra.Command, args []string) {

		configPath = strings.TrimSpace(configPath)
		conf, err := engine.LoadConfig(configPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if !engine.Run(conf, include, exclude) {
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
	flags := rootCmd.Flags()
	flags.StringVarP(&configPath, "config", "c", "-", "Path to config file. - to read from stdin")
	flags.StringVarP(&include, "include", "i", "", "Regular expression to include tests matching id")
	flags.StringVarP(&exclude, "exclude", "x", "", "Regular expression to exclude tests matching id")
}
