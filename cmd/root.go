package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spot-go/spot-go/internal/spotify"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "spot-go",
	Short: "A complete spotify tui",
	Run:   run,
}

func run(cmd *cobra.Command, args []string) {
	ctx := context.Background()
	res := spotify.Login(ctx)
	if res.Err != nil {
		fmt.Println(res.Err)
		os.Exit(1)
	}
	u, err := res.Client.PlayerDevices(ctx)
	if err != nil {
		panic(err)
	}
	b, _ := json.MarshalIndent(u, "", "  ")
	println(string(b))
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
