//go:build linux

package media

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const testName = "org.mpris.MediaPlayer2.spotgo.test"

func TestMetadata(t *testing.T) {
	soWhat := dbus.ObjectPath(trackPrefix + hex.EncodeToString([]byte("spotify:track:so-what")))
	rock := dbus.ObjectPath(trackPrefix + hex.EncodeToString([]byte("Rock")))
	for _, tt := range []struct {
		name string
		info Info
		want map[string]dbus.Variant
	}{
		{"nothing", Info{}, map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(noTrack)}},
		{"song", Info{ID: "spotify:track:so-what", Title: "So What", Artist: "Miles Davis", Duration: 562 * time.Second}, map[string]dbus.Variant{
			"mpris:trackid": dbus.MakeVariant(soWhat),
			"xesam:title":   dbus.MakeVariant("So What"),
			"xesam:artist":  dbus.MakeVariant([]string{"Miles Davis"}),
			"mpris:length":  dbus.MakeVariant(int64(562000000)),
		}},
		{"song with its artwork", Info{ID: "spotify:track:so-what", Title: "So What", Artwork: "https://i.scdn.co/image/kind-of-blue"}, map[string]dbus.Variant{
			"mpris:trackid": dbus.MakeVariant(soWhat),
			"xesam:title":   dbus.MakeVariant("So What"),
			"mpris:artUrl":  dbus.MakeVariant("https://i.scdn.co/image/kind-of-blue"),
		}},
		{"playlist without a song", Info{Title: "Rock"}, map[string]dbus.Variant{
			"mpris:trackid": dbus.MakeVariant(rock),
			"xesam:title":   dbus.MakeVariant("Rock"),
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := metadata(tt.info)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("metadata = %v, want %v", got, tt.want)
			}
			if path := got["mpris:trackid"].Value().(dbus.ObjectPath); !path.IsValid() {
				t.Fatalf("track id %q is not a valid object path", path)
			}
		})
	}
}

func TestPosition(t *testing.T) {
	at := time.Now()
	for _, tt := range []struct {
		name string
		info Info
		now  time.Time
		want time.Duration
	}{
		{"playing moves on", Info{Elapsed: time.Minute, Playing: true}, at.Add(5 * time.Second), 65 * time.Second},
		{"paused stays", Info{Elapsed: time.Minute}, at.Add(5 * time.Second), time.Minute},
		{"stops at the end", Info{Elapsed: time.Minute, Duration: 62 * time.Second, Playing: true}, at.Add(time.Hour), 62 * time.Second},
		{"paused past the end", Info{Elapsed: 70 * time.Second, Duration: 62 * time.Second}, at.Add(5 * time.Second), 62 * time.Second},
	} {
		if got := position(tt.info, at, tt.now); got != tt.want.Microseconds() {
			t.Errorf("%s: position = %d, want %d", tt.name, got, tt.want.Microseconds())
		}
	}
}

func TestPlayerMethodsSendCommands(t *testing.T) {
	got := make(chan Command, 1)
	useHandler(t, func(c Command) { got <- c })

	for _, tt := range []struct {
		name string
		call func() *dbus.Error
		want Command
	}{
		{"PlayPause", mprisPlayer{}.PlayPause, TogglePlayPause},
		{"Play", mprisPlayer{}.Play, Play},
		{"Pause", mprisPlayer{}.Pause, Pause},
		{"Stop", mprisPlayer{}.Stop, Pause},
		{"Next", mprisPlayer{}.Next, NextTrack},
		{"Previous", mprisPlayer{}.Previous, PreviousTrack},
	} {
		if err := tt.call(); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		select {
		case c := <-got:
			if c != tt.want {
				t.Fatalf("%s sent %d, want %d", tt.name, c, tt.want)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s sent nothing", tt.name)
		}
	}
}

func privateBus(t *testing.T) string {
	t.Helper()

	address, _ := privateBusDaemon(t)
	return address
}

// privateBusDaemon starts a session bus for the test, and returns its address
// and the process that runs it.
func privateBusDaemon(t *testing.T) (string, *os.Process) {
	t.Helper()

	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	address, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(address), cmd.Process
}

func connectTo(t *testing.T, address string) *dbus.Conn {
	t.Helper()

	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func resetMPRIS(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		mprisMu.Lock()
		props, published, publishedAt = nil, Info{}, time.Time{}
		connected, connectErr, lost = make(chan struct{}), nil, nil
		mprisMu.Unlock()
	})
}

func property(t *testing.T, obj dbus.BusObject, name string) any {
	t.Helper()

	v, err := obj.GetProperty(name)
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	return v.Value()
}

func TestMPRISOverDBus(t *testing.T) {
	address := privateBus(t)
	resetMPRIS(t)
	p, err := serve(connectTo(t, address), testName)
	if err != nil {
		t.Fatal(err)
	}
	mprisMu.Lock()
	props = p
	mprisMu.Unlock()
	got := make(chan Command, 1)
	useHandler(t, func(c Command) { got <- c })

	client := connectTo(t, address)
	if err := client.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus.Properties"), dbus.WithMatchMember("PropertiesChanged")); err != nil {
		t.Fatal(err)
	}
	signals := make(chan *dbus.Signal, 10)
	client.Signal(signals)
	player := client.Object(testName, objectPath)

	if got := property(t, player, rootInterface+".Identity"); got != "spot-go" {
		t.Fatalf("Identity = %v, want spot-go", got)
	}
	if got := property(t, player, playerInterface+".PlaybackStatus"); got != "Stopped" {
		t.Fatalf("PlaybackStatus = %v, want Stopped before anything is published", got)
	}

	if err := player.Call(playerInterface+".PlayPause", 0).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-got:
		if c != TogglePlayPause {
			t.Fatalf("PlayPause sent %d, want %d", c, TogglePlayPause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PlayPause over D-Bus sent nothing")
	}
	if err := player.Call(playerInterface+".Seek", 0, int64(1000)).Err; err != nil {
		t.Fatalf("Seek: %v", err)
	}

	Publish(Info{ID: "spotify:track:so-what", Title: "So What", Artist: "Miles Davis", Duration: 562 * time.Second, Elapsed: time.Minute, Playing: true})

	if got := property(t, player, playerInterface+".PlaybackStatus"); got != "Playing" {
		t.Fatalf("PlaybackStatus = %v, want Playing", got)
	}
	meta := property(t, player, playerInterface+".Metadata").(map[string]dbus.Variant)
	if title := meta["xesam:title"].Value(); title != "So What" {
		t.Fatalf("title = %v, want So What", title)
	}
	if pos := property(t, player, playerInterface+".Position").(int64); pos < time.Minute.Microseconds() {
		t.Fatalf("Position = %d, want at least a minute", pos)
	}

	deadline := time.After(2 * time.Second)
	for changed := map[string]bool{}; !changed["PlaybackStatus"] || !changed["Metadata"]; {
		select {
		case s := <-signals:
			if len(s.Body) > 1 && s.Body[0] == playerInterface {
				for name := range s.Body[1].(map[string]dbus.Variant) {
					changed[name] = true
				}
			}
		case <-deadline:
			t.Fatal("PropertiesChanged was not emitted for PlaybackStatus and Metadata")
		}
	}

	var xml string
	if err := player.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<interface name="org.mpris.MediaPlayer2.Player">`, `<method name="Seek">`, `<method name="PlayPause">`, `<signal name="Seeked">`} {
		if !strings.Contains(xml, want) {
			t.Fatalf("introspection lacks %s:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "SeekTo") {
		t.Fatalf("introspection shows the Go method name SeekTo:\n%s", xml)
	}
}

func TestTakenNameIsReported(t *testing.T) {
	address := privateBus(t)
	if _, err := serve(connectTo(t, address), testName); err != nil {
		t.Fatal(err)
	}

	if _, err := serve(connectTo(t, address), testName); err == nil {
		t.Fatal("a second player got the same D-Bus name")
	}
}

func TestConnectShowsWhatWasPublishedBefore(t *testing.T) {
	address := privateBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	resetMPRIS(t)

	Publish(Info{Title: "So What", Elapsed: time.Minute})
	connect()

	mprisMu.Lock()
	connected := props != nil
	mprisMu.Unlock()
	if !connected {
		t.Fatal("connect did not export the player")
	}
	var names []string
	if err := connectTo(t, address).BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatal(err)
	}
	var name string
	for _, n := range names {
		if strings.HasPrefix(n, rootInterface+".spotgo.instance") {
			name = n
		}
	}
	if name == "" {
		t.Fatalf("no spot-go player on the bus: %v", names)
	}
	player := connectTo(t, address).Object(name, objectPath)
	if got := property(t, player, playerInterface+".PlaybackStatus"); got != "Paused" {
		t.Fatalf("PlaybackStatus = %v, want Paused", got)
	}
	meta := property(t, player, playerInterface+".Metadata").(map[string]dbus.Variant)
	if title := meta["xesam:title"].Value(); title != "So What" {
		t.Fatalf("title = %v, want So What", title)
	}
}

func TestPublishAfterTheBusIsGone(t *testing.T) {
	address := privateBus(t)
	resetMPRIS(t)
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	p, err := serve(conn, testName)
	if err != nil {
		t.Fatal(err)
	}
	mprisMu.Lock()
	props = p
	mprisMu.Unlock()
	_ = conn.Close()

	Publish(Info{Title: "So What", Playing: true})
}

func TestPositionMovesWhilePlaying(t *testing.T) {
	address := privateBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	resetMPRIS(t)
	connect()

	Publish(Info{Title: "So What", Elapsed: time.Minute, Duration: time.Hour, Playing: true})
	time.Sleep(1500 * time.Millisecond)

	var names []string
	client := connectTo(t, address)
	if err := client.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if strings.HasPrefix(n, rootInterface+".spotgo.instance") {
			pos := property(t, client.Object(n, objectPath), playerInterface+".Position").(int64)
			if pos < (61 * time.Second).Microseconds() {
				t.Fatalf("Position = %v, want it to move on from 1m while playing", time.Duration(pos)*time.Microsecond)
			}
			return
		}
	}
	t.Fatalf("no spot-go player on the bus: %v", names)
}

func servedPlayer(t *testing.T) dbus.BusObject {
	t.Helper()

	address := privateBus(t)
	resetMPRIS(t)
	p, err := serve(connectTo(t, address), testName)
	if err != nil {
		t.Fatal(err)
	}
	mprisMu.Lock()
	props = p
	mprisMu.Unlock()
	return connectTo(t, address).Object(testName, objectPath)
}

func TestMetadataIsReplacedNotMerged(t *testing.T) {
	player := servedPlayer(t)

	Publish(Info{ID: "spotify:track:so-what", Title: "So What", Artist: "Miles Davis", Duration: 562 * time.Second, Playing: true})
	Publish(Info{Title: "Rock", Playing: true})

	meta := property(t, player, playerInterface+".Metadata").(map[string]dbus.Variant)
	if _, ok := meta["xesam:artist"]; ok {
		t.Fatalf("metadata kept the previous artist: %v", meta)
	}
	if _, ok := meta["mpris:length"]; ok {
		t.Fatalf("metadata kept the previous length: %v", meta)
	}
	if title := meta["xesam:title"].Value(); title != "Rock" {
		t.Fatalf("title = %v, want Rock", title)
	}
}

func TestReadsDuringPublishAreSafe(t *testing.T) {
	player := servedPlayer(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				var all map[string]dbus.Variant
				_ = player.Call(propertiesInterface+".GetAll", 0, playerInterface).Store(&all)
				_, _ = player.GetProperty(playerInterface + ".Metadata")
			}
		}()
	}
	for i := range 300 {
		Publish(Info{ID: "spotify:track:" + string(rune('a'+i%26)), Title: "Song", Artist: "Artist", Duration: time.Minute, Playing: i%2 == 0})
	}
	close(stop)
	wg.Wait()
}

func TestPropertiesAreReadOnly(t *testing.T) {
	player := servedPlayer(t)

	if err := player.SetProperty(playerInterface+".PlaybackStatus", dbus.MakeVariant("Playing")); err == nil {
		t.Fatal("PlaybackStatus could be set over D-Bus")
	}
	if got := property(t, player, playerInterface+".PlaybackStatus"); got != "Stopped" {
		t.Fatalf("PlaybackStatus = %v, want Stopped", got)
	}
}

func TestNoSessionBusStartsNothing(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "launched")
	script := "#!/bin/sh\ntouch " + marker + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "dbus-launch"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	resetMPRIS(t)

	connect()

	mprisMu.Lock()
	connected := props != nil
	mprisMu.Unlock()
	if connected {
		t.Fatal("connected without a session bus")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("dbus-launch was started")
	}
	if _, err := Connected(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Connected() = %v, want %v", err, ErrUnsupported)
	}
}

func TestConnectedEndsWithTheBus(t *testing.T) {
	address, daemon := privateBusDaemon(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	resetMPRIS(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Connected(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connected() before connecting = %v, want it to wait", err)
	}

	connect()
	gone, err := Connected(context.Background())
	if err != nil {
		t.Fatalf("Connected() = %v", err)
	}
	select {
	case <-gone:
		t.Fatal("the bus is gone while it runs")
	default:
	}

	// As when the user logs out.
	_ = daemon.Kill()
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the bus went away and Connected() didn't say so")
	}
}
