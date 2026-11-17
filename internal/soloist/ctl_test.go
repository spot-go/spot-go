package soloist

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlRunsSoloistCtl(t *testing.T) {
	dir := useTempFolder(t)
	calls := filepath.Join(dir, "calls")
	writeScript(t, binaryFile, fakeScript(time.Now(), `echo "$@" >> "`+calls+`"`))

	c := Control{}
	for _, err := range []error{
		c.SetShuffle(context.Background(), true),
		c.SetShuffle(context.Background(), false),
		c.SetRepeat(context.Background(), "context"),
		c.SetRepeat(context.Background(), "off"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	b, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "ctl --data-dir " + dataDir + " "
	want := prefix + "shuffle on\n" + prefix + "shuffle off\n" + prefix + "repeat context\n" + prefix + "repeat off\n"
	if got := string(b); got != want {
		t.Fatalf("calls =\n%s\nwant\n%s", got, want)
	}
}

func TestControlErrors(t *testing.T) {
	useTempFolder(t)

	for _, tt := range []struct {
		name   string
		daemon string
		want   string
	}{
		{"message", "echo connecting... >&2\necho 'Spotify Soloist is not running with --ws' >&2\nexit 2", "Spotify Soloist is not running with --ws"},
		{"silent", "exit 3", "soloist ctl failed: exit status 3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writeScript(t, binaryFile, fakeScript(time.Now(), tt.daemon))

			err := Control{}.SetRepeat(context.Background(), "track")
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestControlStopsWithItsContext(t *testing.T) {
	useTempFolder(t)
	writeScript(t, binaryFile, fakeScript(time.Now(), "sleep 5"))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Control{}.SetShuffle(ctx, true)

	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want %v", err, context.DeadlineExceeded)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("SetShuffle took %v, want it to stop with its context", elapsed)
	}
}

func TestDaemonListensForLocalControlOnly(t *testing.T) {
	useTempFolder(t)

	args := strings.Join(daemonArgs("key"), " ")
	if !strings.Contains(args, "--ws 127.0.0.1:0") {
		t.Fatalf("daemon args = %q, want the WebSocket API on 127.0.0.1 only", args)
	}
}
