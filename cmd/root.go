package cmd

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/program"
	"github.com/spot-go/spot-go/internal/spotifyd"
	"github.com/spot-go/spot-go/internal/ui/views/login"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "spot-go",
	Short: "A complete spotify tui",
	Run:   run,
}

func run(cmd *cobra.Command, args []string) {
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

	spotifyd.KillStale()
	lipgloss.SetHasDarkBackground(termenv.HasDarkBackground())
	p := tea.NewProgram(login.NewModel(), tea.WithAltScreen())
	program.Instance = p
	_, err := p.Run()
	spotifyd.Stop()
	if err != nil {
		log.Fatal(err)
	}
}

func loadSettings() error {
	if err := config.Load(); err != nil {
		return err
	}
	return spotifyd.CreateConfigFolder()
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
