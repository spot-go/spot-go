package process

import (
	"regexp"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		line string
		want Event
	}{
		{"[INFO] Authenticated as 'francisco' !", Authenticated},
		{`Authenticated as "francisco" !`, Authenticated},
		{"[ERROR] connection to spotify failed: Service unavailable", Other},
		{"[ERROR] connection to spotify failed: Permission denied { Login failed with reason: Bad credentials }", Failed},
		{"[ERROR] connection to spotify failed: Permission denied { Login failed with reason: Premium account required }", Failed},
		{"[ERROR] connection to spotify failed: Permission denied { Login failed with reason: Try another access point }", Other},
		{"[WARN] Instructed to try another access point...", Other},
		{"[ERROR] Tried too many access points", Other},
		{"[INFO] retrying connection in 5 seconds (retry 1/4)", Other},
		{"[INFO] Login via OAuth as user me.", Other},
		{"[ERROR] failed to enable discovery: address in use", Other},
		{"Error: ", Other},
		{"   0: failed to connect to spotify", Failed},
		{"   0: Discovery unavailable and no credentials found.", Failed},
		{"error: unexpected argument '-u' found", Failed},
		{"dyld[123]: Library not loaded: /opt/homebrew/opt/portaudio/lib/libportaudio.2.dylib", Failed},
		{`[ERROR] librespot does not support "free" accounts.`, Failed},
		{"[ERROR] could not initialize spirc: Service unavailable {503}", Failed},
		{"[INFO] Starting zeroconf server to advertise on local network.", Other},
		{"[WARN] Failed reading config file", Other},
		{"Browse to: https://accounts.spotify.com/authorize?x=1", Other},
		{"", Other},
	}
	for _, tt := range tests {
		if got := Classify(tt.line); got != tt.want {
			t.Errorf("Classify(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestMessage(t *testing.T) {
	tests := map[string]string{
		"[ERROR] connection to spotify failed: Permission denied { Login failed with reason: Bad credentials }": "connection to spotify failed: Permission denied { Login failed with reason: Bad credentials }",
		"   0: Discovery unavailable and no credentials found.":                                                 "Discovery unavailable and no credentials found.",
		"   12: failed to connect to spotify ":                                                                  "failed to connect to spotify",
		`[ERROR] librespot does not support "free" accounts.`:                                                   `librespot does not support "free" accounts.`,
		"error: unexpected argument '-u' found":                                                                 "error: unexpected argument '-u' found",
		"dyld[123]: Library not loaded: /opt/homebrew/opt/portaudio/lib/libportaudio.2.dylib":                   "dyld[123]: Library not loaded: /opt/homebrew/opt/portaudio/lib/libportaudio.2.dylib",
		"[ERROR] could not initialize spirc: Service unavailable {503}":                                         "could not initialize spirc: Service unavailable {503}",
	}
	for in, want := range tests {
		if got := Message(in); got != want {
			t.Errorf("Message(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAuthURL(t *testing.T) {
	tests := []struct {
		line   string
		want   string
		wantOK bool
	}{
		{"Browse to: https://accounts.spotify.com/authorize?client_id=x ", "https://accounts.spotify.com/authorize?client_id=x", true},
		{"[INFO] Browse to: https://a.b/c", "https://a.b/c", true},
		{"Browse to: ", "", false},
		{"[INFO] OAuth server listening on 127.0.0.1:8000", "", false},
	}
	for _, tt := range tests {
		got, ok := AuthURL(tt.line)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("AuthURL(%q) = %q, %v, want %q, %v", tt.line, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in     string
		want   Version
		wantOK bool
	}{
		{"spotifyd 0.4.2\n", Version{0, 4, 2}, true},
		{"spotifyd 0.3.4", Version{0, 3, 4}, true},
		{"spotifyd 1.10.0-beta", Version{1, 10, 0}, true},
		{"dyld: Library not loaded", Version{}, false},
		{"", Version{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseVersion(tt.in)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("ParseVersion(%q) = %v, %v, want %v, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	required := Version{0, 4, 2}
	tests := []struct {
		v    Version
		want bool
	}{
		{Version{0, 4, 2}, true},
		{Version{0, 4, 3}, true},
		{Version{0, 5, 0}, true},
		{Version{1, 0, 0}, true},
		{Version{0, 4, 1}, false},
		{Version{0, 3, 9}, false},
	}
	for _, tt := range tests {
		if got := tt.v.AtLeast(required); got != tt.want {
			t.Errorf("%v.AtLeast(%v) = %v, want %v", tt.v, required, got, tt.want)
		}
	}
}

func TestStalePattern(t *testing.T) {
	folder := "/Users/me/Library/Application Support/spot-go (1)/spotifyd"
	pattern := regexp.MustCompile(StalePattern(folder))

	tests := []struct {
		cmdline string
		want    bool
	}{
		{"/opt/homebrew/bin/spotifyd --no-daemon --cache-path " + folder + " --config-path " + folder + "/spotifyd.conf", true},
		{"/opt/homebrew/bin/spotifyd authenticate --cache-path " + folder + " --oauth-port 51234", true},
		{folder + "/spotifyd --no-daemon --cache-path " + folder, true},
		{"vim " + folder + "/spotifyd.conf", false},
		{"tail -f " + folder + "/spotifyd.conf", false},
		{"/opt/homebrew/bin/spotifyd --no-daemon --cache-path " + folder + "-other", false},
		{"/opt/homebrew/bin/spotifyd --no-daemon", false},
	}
	for _, tt := range tests {
		if got := pattern.MatchString(tt.cmdline); got != tt.want {
			t.Errorf("match(%q) = %v, want %v", tt.cmdline, got, tt.want)
		}
	}
}
