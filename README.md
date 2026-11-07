# spot-go

A Spotify terminal UI.

## Requirements

- Go 1.20 or newer
- A Spotify Premium account
- [spotifyd](https://github.com/Spotifyd/spotifyd) 0.4.2, which plays the audio:
  - macOS (Apple Silicon or Intel): install it with `brew install spotifyd`
  - Linux (x86_64, aarch64 or armv7): spot-go downloads the official 0.4.2 release on the first run

## Running

```bash
go run .
```

On the first run spot-go asks you to log in twice in the browser:

1. The Spotify Web API, used to read your playlists and control playback.
2. spotifyd, so it can play on your account. You can instead connect from a Spotify app on the same local network, but that may not work with current Spotify apps.

## Keys

- `←` and `→` switch tabs.
- `↑` and `↓` move in the focused list.
- `tab` moves between the playlists and the songs of the selected playlist.
- `enter` plays the selected playlist from the start, or the selected song.
- `/` filters the playlists.
- `r` reloads the songs of the selected playlist.

Spotify may only list the songs of playlists you own or collaborate on, depending on the mode of the Spotify app. Other playlists still play with `enter`.

## No sound

If the status line shows `No sound: spotifyd got no audio key from Spotify`, Spotify refused to give spotifyd the key that decrypts the audio. This happens on some accounts and is [librespot issue #1649](https://github.com/librespot-org/librespot/issues/1649). spotifyd has no fix for it yet.

## Debugging

Set `DEBUG=1` to write a `debug.log` file. `make debug` starts the app under Delve on port 2345.

## Spotify app settings

spot-go uses the client ID in `internal/spotify/auth.go`. The Spotify app behind it must have this exact redirect URI registered in the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard):

```
http://127.0.0.1:10888/callback
```

Apps in development mode only work for up to 5 users added to the app's user list, and the app owner needs Spotify Premium.

## License

spot-go is MIT licensed. spotifyd is GPL-3.0 licensed: spot-go never ships or links it, it only runs the spotifyd binary you installed or that spot-go downloaded from the official release.
