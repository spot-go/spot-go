package cmd

import (
	"context"
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
	c := context.Background()
	res := spotify.Login(c)
	if res.Err != nil {
		fmt.Println(res.Err)
		os.Exit(1)
	}
	u, err := res.Client.CurrentUser(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Printf("client %+v\n", u)
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
