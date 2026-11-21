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
	"github.com/franciscocpg/spot-go/internal/background"
	"github.com/franciscocpg/spot-go/internal/config"
	"github.com/franciscocpg/spot-go/internal/media"
	"github.com/franciscocpg/spot-go/internal/program"
	"github.com/franciscocpg/spot-go/internal/soloist"
	"github.com/franciscocpg/spot-go/internal/ui/styles"
	"github.com/franciscocpg/spot-go/internal/ui/views/login"
	"github.com/franciscocpg/spot-go/internal/ui/views/player"
	"github.com/spf13/cobra"
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

	// Before spot-go reads the Spotify token, which the spot-go an earlier
	// run left in the background could refresh meanwhile.
	followed := background.Stop()

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
	quitSoloist(keepPlaying(final), followed)
	// Until Soloist stopped, a hang-up only quits spot-go, which already
	// quit, and doesn't end spot-go before it stopped Soloist.
	stopHangup()
	if err != nil && !errors.Is(err, tea.ErrInterrupted) {
		log.Fatal(err)
	}
}

// quitSoloist stops Soloist as spot-go quits, or leaves it playing when keep
// is true. A Soloist left playing gets spot-go again, in the background, to
// keep Now Playing and the media keys on it: the one this run leaves, or
// followed, the one spot-go in the background followed as this run started,
// when this run quit before it took it over. The Soloist another run of
// spot-go plays on, which no spot-go in the background followed, gets none.
func quitSoloist(keep bool, followed int) {
	// Before Detach or Stop, while this run may still know the device.
	device := soloist.DeviceID()
	detached := 0
	if keep {
		detached = soloist.Detach()
	} else {
		soloist.Stop()
	}

	left, ok := soloist.LeftRunning()
	if !ok || left != detached && left != followed {
		return
	}
	if err := background.Start(left, device); err != nil {
		fmt.Fprintln(os.Stderr, "spot-go: the media keys won't reach the music left playing:", err)
	}
}

type keeper interface {
	// KeepPlaying reports whether Soloist keeps playing after spot-go quits.
	KeepPlaying() bool
}

// The program's model is the login view until the player shows, and then
// the player.
var (
	_ keeper = login.Model{}
	_ keeper = player.Model{}
)

// keepPlaying reports whether Soloist keeps playing after spot-go quits: the
// user chose so when quitting the player, or spot-go found it playing and
// quit before the player showed.
func keepPlaying(final tea.Model) bool {
	k, ok := final.(keeper)
	return ok && k.KeepPlaying()
}

// quitOnHangup quits p when the terminal closes, as Soloist runs apart from
// the terminal and would otherwise keep playing. A Soloist an earlier run of
// spot-go left playing still keeps playing if the player didn't show yet, as
// with any other quit. The function it returns stops listening.
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
