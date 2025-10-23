package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "block-benchmark",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("http-rpc", "", "http://127.0.0.1:8545", "RPC HTTP Endpoint")
	rootCmd.PersistentFlags().StringP("ws-rpc", "", "ws://127.0.0.1:8546", "RPC WS Endpoint")
	rootCmd.PersistentFlags().StringP("rpc-file", "", "", "Path to a file that contains RPC HTTP endpoints, one per line starting from line 2")
	rootCmd.PersistentFlags().StringP("cl-address", "", "", "CL address for validator")
	rootCmd.PersistentFlags().IntP("mempool", "", 5000, "Mempool size")
}
