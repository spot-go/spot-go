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
	ctx := context.Background()
	client, err := spotify.Login(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	u, err := client.CurrentUser(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("u %+v\n", u)

	// p, err := client.PlayerCurrentlyPlaying(ctx)
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Printf("u %+v\n", p.Item.Duration)
	// fmt.Printf("u %+v\n", p.Progress)
	// id := libspotify.ID("ecbb34ecaaa7f5b84c61cc8a912e7c913e6930fd")
	// err = res.Client.TransferPlayback(ctx, id, true)
	// if err != nil {
	// 	panic(err)
	// }
	// err = res.Client.PlayOpt(ctx, &libspotify.PlayOptions{
	// 	DeviceID: &id,
	// })
	// if err != nil {
	// 	panic(err)
	// }
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
