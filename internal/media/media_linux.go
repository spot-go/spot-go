//go:build linux

package media

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

type (
	mprisRoot   struct{}
	mprisPlayer struct{}

	mprisProperties struct {
		conn   *dbus.Conn
		mu     sync.RWMutex
		values map[string]map[string]dbus.Variant
	}
)

const (
	objectPath          = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	rootInterface       = "org.mpris.MediaPlayer2"
	playerInterface     = "org.mpris.MediaPlayer2.Player"
	propertiesInterface = "org.freedesktop.DBus.Properties"
	noTrack             = dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")
	trackPrefix         = "/io/github/spotgo/track/"
)

var (
	playerMethods = map[string]string{"SeekTo": "Seek"}

	connectOnce sync.Once
	mprisMu     sync.Mutex
	props       *mprisProperties
	published   Info
	publishedAt time.Time
)

func Main(run func()) {
	run()
}

func Listen(fn func(Command)) {
	setHandler(fn)
	connectOnce.Do(func() { go connect() })
}

func Publish(info Info) {
	now := time.Now()
	mprisMu.Lock()
	published, publishedAt = info, now
	p := props
	mprisMu.Unlock()
	if p != nil {
		p.update(info, now, now)
	}
}

func connect() {
	conn, err := sessionBus()
	if err != nil {
		return
	}
	p, err := serve(conn, fmt.Sprintf("%s.spotgo.instance%d", rootInterface, os.Getpid()))
	if err != nil {
		_ = conn.Close()
		return
	}

	mprisMu.Lock()
	props = p
	info, at := published, publishedAt
	mprisMu.Unlock()
	if !at.IsZero() {
		p.update(info, at, time.Now())
	}
	go p.trackPosition()
}

func sessionBus() (*dbus.Conn, error) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func serve(conn *dbus.Conn, name string) (*mprisProperties, error) {
	p := &mprisProperties{
		conn: conn,
		values: map[string]map[string]dbus.Variant{
			rootInterface: variants(map[string]any{
				"CanQuit":             false,
				"CanRaise":            false,
				"HasTrackList":        false,
				"Identity":            "spot-go",
				"SupportedUriSchemes": []string{},
				"SupportedMimeTypes":  []string{},
			}),
			playerInterface: variants(map[string]any{
				"PlaybackStatus": "Stopped",
				"Metadata":       metadata(Info{}),
				"Position":       int64(0),
				"Rate":           1.0,
				"MinimumRate":    1.0,
				"MaximumRate":    1.0,
				"Volume":         1.0,
				"CanGoNext":      true,
				"CanGoPrevious":  true,
				"CanPlay":        true,
				"CanPause":       true,
				"CanSeek":        false,
				"CanControl":     true,
			}),
		},
	}

	for _, export := range []struct {
		v     any
		names map[string]string
		iface string
	}{
		{mprisRoot{}, nil, rootInterface},
		{mprisPlayer{}, playerMethods, playerInterface},
		{p, nil, propertiesInterface},
	} {
		if err := conn.ExportWithMap(export.v, export.names, objectPath, export.iface); err != nil {
			return nil, err
		}
	}

	node := &introspect.Node{
		Name: string(objectPath),
		Interfaces: []introspect.Interface{
			prop.IntrospectData,
			{Name: rootInterface, Methods: introspect.Methods(mprisRoot{}), Properties: p.introspection(rootInterface)},
			{
				Name:       playerInterface,
				Methods:    dbusMethods(mprisPlayer{}, playerMethods),
				Properties: p.introspection(playerInterface),
				Signals:    []introspect.Signal{{Name: "Seeked", Args: []introspect.Arg{{Name: "Position", Type: "x"}}}},
			},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), objectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, err
	}

	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("D-Bus name %s is taken", name)
	}
	return p, nil
}

func variants(values map[string]any) map[string]dbus.Variant {
	out := make(map[string]dbus.Variant, len(values))
	for name, v := range values {
		out[name] = dbus.MakeVariant(v)
	}
	return out
}

func dbusMethods(v any, names map[string]string) []introspect.Method {
	methods := introspect.Methods(v)
	for i, m := range methods {
		if name, ok := names[m.Name]; ok {
			methods[i].Name = name
		}
	}
	return methods
}

func (p *mprisProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	values, ok := p.values[iface]
	if !ok {
		return dbus.Variant{}, prop.ErrIfaceNotFound
	}
	v, ok := values[name]
	if !ok {
		return dbus.Variant{}, prop.ErrPropNotFound
	}
	return v, nil
}

func (p *mprisProperties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	values, ok := p.values[iface]
	if !ok {
		return nil, prop.ErrIfaceNotFound
	}
	return maps.Clone(values), nil
}

func (p *mprisProperties) Set(string, string, dbus.Variant) *dbus.Error {
	return prop.ErrReadOnly
}

func (p *mprisProperties) introspection(iface string) []introspect.Property {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var out []introspect.Property
	for _, name := range slices.Sorted(maps.Keys(p.values[iface])) {
		out = append(out, introspect.Property{Name: name, Type: p.values[iface][name].Signature().String(), Access: "read"})
	}
	return out
}

func (p *mprisProperties) update(info Info, at, now time.Time) {
	changed := variants(map[string]any{
		"Metadata":       metadata(info),
		"PlaybackStatus": playbackStatus(info),
	})
	p.mu.Lock()
	maps.Copy(p.values[playerInterface], changed)
	p.values[playerInterface]["Position"] = dbus.MakeVariant(position(info, at, now))
	p.mu.Unlock()
	_ = p.conn.Emit(objectPath, propertiesInterface+".PropertiesChanged", playerInterface, changed, []string{})
}

func (p *mprisProperties) setPosition(v int64) {
	p.mu.Lock()
	p.values[playerInterface]["Position"] = dbus.MakeVariant(v)
	p.mu.Unlock()
}

func (p *mprisProperties) trackPosition() {
	for range time.Tick(time.Second) {
		mprisMu.Lock()
		info, at := published, publishedAt
		mprisMu.Unlock()
		if info.Playing {
			p.setPosition(position(info, at, time.Now()))
		}
	}
}

func metadata(info Info) map[string]dbus.Variant {
	m := map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(trackID(info))}
	if info.Title != "" {
		m["xesam:title"] = dbus.MakeVariant(info.Title)
	}
	if info.Artist != "" {
		m["xesam:artist"] = dbus.MakeVariant([]string{info.Artist})
	}
	if info.Duration > 0 {
		m["mpris:length"] = dbus.MakeVariant(info.Duration.Microseconds())
	}
	return m
}

func trackID(info Info) dbus.ObjectPath {
	key := info.ID
	if key == "" {
		key = info.Title
	}
	if key == "" {
		return noTrack
	}
	return dbus.ObjectPath(trackPrefix + hex.EncodeToString([]byte(key)))
}

func playbackStatus(info Info) string {
	if info.Playing {
		return "Playing"
	}
	return "Paused"
}

func position(info Info, at, now time.Time) int64 {
	elapsed := info.Elapsed
	if info.Playing {
		elapsed += now.Sub(at)
	}
	if info.Duration > 0 {
		elapsed = min(elapsed, info.Duration)
	}
	return elapsed.Microseconds()
}

func (mprisRoot) Raise() *dbus.Error { return nil }

func (mprisRoot) Quit() *dbus.Error { return nil }

func (mprisPlayer) Next() *dbus.Error { return send(NextTrack) }

func (mprisPlayer) Previous() *dbus.Error { return send(PreviousTrack) }

func (mprisPlayer) Pause() *dbus.Error { return send(Pause) }

func (mprisPlayer) PlayPause() *dbus.Error { return send(TogglePlayPause) }

func (mprisPlayer) Stop() *dbus.Error { return send(Pause) }

func (mprisPlayer) Play() *dbus.Error { return send(Play) }

func (mprisPlayer) SeekTo(int64) *dbus.Error { return nil }

func (mprisPlayer) SetPosition(dbus.ObjectPath, int64) *dbus.Error { return nil }

func (mprisPlayer) OpenUri(string) *dbus.Error { return nil }

func send(c Command) *dbus.Error {
	dispatch(c)
	return nil
}
