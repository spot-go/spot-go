package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/program"
	"github.com/spot-go/spot-go/internal/soloist"
	"github.com/spot-go/spot-go/internal/ui/styles"
	"github.com/spot-go/spot-go/internal/ui/views/login"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "spot-go",
	Short: "A complete spotify tui",
	Run:   run,
}

func run(cmd *cobra.Command, args []string) {
	if !soloist.Supported() {
		fmt.Printf("fatal: Soloist has no build for %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(1)
	}

	if err := loadSettings(); err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}

	if len(os.Getenv("DEBUG")) > 0 {
		f, err := tea.LogToFile("debug.log", "debug")
		if err != nil {
			fmt.Println("fatal:", err)
			os.Exit(1)
		}
		defer f.Close()
	}

	soloist.KillStale()
	styles.HasDarkBackground = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	p := tea.NewProgram(login.NewModel(media.Publish))
	program.Instance = p
	media.Listen(func(c media.Command) { p.Send(c) })
	_, err := p.Run()
	soloist.Stop()
	if err != nil && !errors.Is(err, tea.ErrInterrupted) {
		log.Fatal(err)
	}
}

func loadSettings() error {
	if err := config.Load(); err != nil {
		return err
	}
	return soloist.CreateFolder()
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
