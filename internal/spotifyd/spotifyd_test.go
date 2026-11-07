package spotifyd

import (
	"errors"
	"testing"
)

func drainPlaybackErrors() {
	for {
		select {
		case <-playbackErrors:
		default:
			return
		}
	}
}

func TestReportPlaybackError(t *testing.T) {
	drainPlaybackErrors()
	defer drainPlaybackErrors()

	reportPlaybackError("[INFO librespot_playback::player] Loading <Fogaréu> with Spotify URI <spotify:track:x>")
	select {
	case err := <-PlaybackErrors():
		t.Fatalf("an info line was reported as a playback error: %v", err)
	default:
	}

	reportPlaybackError("[\x1b[31mERROR\x1b[0m librespot_core::audio_key] error audio key 0 1")
	reportPlaybackError("[\x1b[33mWARN\x1b[0m librespot_playback::player] Unable to load key, continuing without decryption: Service unavailable { audio key error }")

	select {
	case err := <-PlaybackErrors():
		if !errors.Is(err, ErrNoAudioKey) {
			t.Fatalf("err = %v, want %v", err, ErrNoAudioKey)
		}
	default:
		t.Fatal("the audio key error was not reported")
	}
	select {
	case err := <-PlaybackErrors():
		t.Fatalf("a second report was queued while nobody listened: %v", err)
	default:
	}
}
