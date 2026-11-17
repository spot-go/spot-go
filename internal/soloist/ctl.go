package soloist

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// wsAddr is where Soloist listens for local control: only on this computer,
// on a port the operating system picks. `soloist ctl` finds the port in the
// data folder.
const wsAddr = "127.0.0.1:0"

const ctlWaitDelay = 500 * time.Millisecond

// Control changes shuffle and repeat through Soloist itself, with `soloist
// ctl`. The Spotify Web API can't change them on Soloist: Spotify answers
// "Restriction violated".
type Control struct{}

// SetShuffle turns shuffle on or off.
func (Control) SetShuffle(ctx context.Context, on bool) error {
	state := "off"
	if on {
		state = "on"
	}
	return ctl(ctx, "shuffle", state)
}

// SetRepeat sets the repeat mode: off, context or track.
func (Control) SetRepeat(ctx context.Context, mode string) error {
	return ctl(ctx, "repeat", mode)
}

func ctl(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, binaryFile, append([]string{"ctl", "--data-dir", dataDir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// Once the context ends, don't wait long for what it started to close
	// its output.
	cmd.WaitDelay = ctlWaitDelay
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if text := lastLine(out.String()); text != "" {
		return errors.New(text)
	}
	return fmt.Errorf("soloist ctl failed: %w", err)
}

// lastLine is the last line of out that isn't blank, which is where `soloist
// ctl` says why it failed.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
