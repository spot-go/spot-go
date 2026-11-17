package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

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

	styles.HasDarkBackground = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	p := tea.NewProgram(login.NewModel(media.Publish))
	program.Instance = p
	media.Listen(func(c media.Command) { p.Send(c) })
	stopHangup := quitOnHangup(p)
	final, err := p.Run()
	stopHangup()
	if keepPlaying(final) {
		soloist.Detach()
	} else {
		soloist.Stop()
	}
	if err != nil && !errors.Is(err, tea.ErrInterrupted) {
		log.Fatal(err)
	}
}

// keepPlaying reports whether Soloist keeps playing after spot-go quits, as
// the user chose when quitting.
func keepPlaying(final tea.Model) bool {
	k, ok := final.(interface{ KeepPlaying() bool })
	return ok && k.KeepPlaying()
}

// quitOnHangup quits p when the terminal closes, as Soloist runs apart from
// the terminal and would otherwise keep playing. The function it returns
// stops listening.
func quitOnHangup(p *tea.Program) func() {
	hangup := make(chan os.Signal, 1)
	signal.Notify(hangup, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case <-hangup:
			p.Quit()
		case <-done:
		}
	}()
	return func() {
		signal.Stop(hangup)
		close(done)
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
