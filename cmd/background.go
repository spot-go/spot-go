package cmd

import (
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spot-go/spot-go/internal/background"
)

var (
	backgroundSoloist int
	backgroundDevice  string

	// backgroundCmd is spot-go once it quit and left Soloist playing: it
	// keeps Now Playing and the media keys on the music, and the next run of
	// spot-go stops it.
	backgroundCmd = &cobra.Command{
		Use:          background.Command,
		Short:        "Keep Now Playing and the media keys on the music spot-go left playing",
		Hidden:       true,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
			defer stop()
			return background.Run(ctx, backgroundSoloist, backgroundDevice)
		},
	}
)

func init() {
	backgroundCmd.Flags().IntVar(&backgroundSoloist, background.SoloistFlag, 0, "process ID of the Soloist left playing")
	backgroundCmd.Flags().StringVar(&backgroundDevice, background.DeviceFlag, "", "device ID of that Soloist")
	_ = backgroundCmd.MarkFlagRequired(background.SoloistFlag)

	rootCmd.AddCommand(backgroundCmd)
	// The command is hidden, so spot-go shows no commands to help with or
	// complete.
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
}
