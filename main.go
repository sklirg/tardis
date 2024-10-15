package main

import (
	"os"

	"github.com/sklirg/tardis/cmd/migrate"
	"github.com/sklirg/tardis/cmd/tardis"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tardis",
	Short: "",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		tardis.Run()
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "migrate",
	Short: "run migrations",
	Long:  `Run database migrations for tardis`,
	Run: func(cmd *cobra.Command, args []string) {
		migrate.Migrate()
	},
}
