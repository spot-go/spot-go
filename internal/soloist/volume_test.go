package soloist

import (
	"os"
	"slices"
	"strconv"
	"testing"
)

func initialVolume(t *testing.T) (string, bool) {
	t.Helper()

	args := daemonArgs("key")
	i := slices.Index(args, "--initial-volume")
	if i < 0 {
		return "", false
	}
	if i+1 >= len(args) {
		t.Fatalf("--initial-volume has no value: %q", args)
	}
	return args[i+1], true
}

func TestDaemonStartsAtTheSavedVolume(t *testing.T) {
	for _, percent := range []int{0, 55, 100} {
		want := strconv.Itoa(percent)
		t.Run(want, func(t *testing.T) {
			useTempFolder(t)

			if err := SaveVolume(percent); err != nil {
				t.Fatal(err)
			}

			if got, ok := initialVolume(t); !ok || got != want {
				t.Fatalf("--initial-volume = %q, %v, want %q", got, ok, want)
			}
		})
	}
}

func TestDaemonKeepsTheDefaultVolumeWithoutASavedOne(t *testing.T) {
	useTempFolder(t)

	if got, ok := initialVolume(t); ok {
		t.Fatalf("--initial-volume = %q, want no flag", got)
	}
}

func TestDaemonIgnoresAnInvalidSavedVolume(t *testing.T) {
	for _, content := range []string{"", "\n", "loud", "-1", "101", "5.5"} {
		t.Run(strconv.Quote(content), func(t *testing.T) {
			useTempFolder(t)

			if err := os.WriteFile(volumeFile, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}

			if got, ok := initialVolume(t); ok {
				t.Fatalf("--initial-volume = %q, want no flag", got)
			}
		})
	}
}

func TestSaveVolumeReplacesTheSavedOne(t *testing.T) {
	useTempFolder(t)

	for _, percent := range []int{100, 5} {
		if err := SaveVolume(percent); err != nil {
			t.Fatal(err)
		}
	}

	if got, ok := initialVolume(t); !ok || got != "5" {
		t.Fatalf("--initial-volume = %q, %v, want 5", got, ok)
	}
}

func TestSaveVolumeRejectsAnInvalidVolume(t *testing.T) {
	useTempFolder(t)

	for _, percent := range []int{-1, 101} {
		if err := SaveVolume(percent); err == nil {
			t.Fatalf("SaveVolume(%d) = nil, want an error", percent)
		}
	}
	if _, err := os.Stat(volumeFile); !os.IsNotExist(err) {
		t.Fatalf("volume file exists after invalid volumes: %v", err)
	}
}
