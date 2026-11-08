# spot-go

<p align="center">
  <img src="assets/logo.png" alt="spot-go logo: a Go Gopher wearing headphones with a green treble clef" width="200">
</p>

A Spotify terminal UI.

## Requirements

- Go 1.27.3 or newer. `mise.toml` pins this version, so with [mise](https://mise.jdx.dev) you can install it with `mise install`.
- A Spotify Premium account
- A program that plays the audio. spot-go uses [Soloist](https://developer.spotify.com/documentation/soloist), Spotify's own player, on:
  - macOS on Apple Silicon
  - Linux on x86_64, aarch64 or armv7, with PipeWire or PulseAudio

  spot-go downloads Soloist from Spotify on the first run. On Intel Macs, use [spotifyd](https://github.com/Spotifyd/spotifyd) 0.4.2 instead (see [spotifyd](#spotifyd)).

## Running

```bash
go run .
```

With mise, run it with the pinned Go:

```bash
mise exec -- go run .
```

On the first run spot-go:

1. Asks you to log in to the Spotify Web API in the browser. spot-go uses it to read your playlists and control playback.
2. Downloads Soloist. On macOS it only installs a binary signed by Spotify.
3. Asks for a Soloist API key. Create one at the [Soloist dashboard](https://developer.spotify.com/dashboard/soloist) and paste it. spot-go saves it only on your computer, in its config folder.
4. Shows the device name, `spot-go@<hostname>`. Open Spotify on a phone or computer on the same network, open the list of devices and select it. Soloist remembers the login, so you only do this once.

Each Soloist build stops working 90 days after it was built. spot-go downloads a newer one when the current build has less than 14 days left.

The API key is passed to Soloist on its command line, so other users of the same computer can see it in `ps`.

## spotifyd

Run `go run . --player spotifyd` to play with spotifyd instead of Soloist:

- macOS (Apple Silicon or Intel): install it with `brew install spotifyd`
- Linux (x86_64, aarch64 or armv7): spot-go downloads the official 0.4.2 release on the first run

spot-go asks you to log in to spotifyd in the browser. You can instead connect from a Spotify app on the same local network, but that may not work with current Spotify apps.

If the status line shows `No sound: spotifyd got no audio key from Spotify`, Spotify refused to give spotifyd the key that decrypts the audio. This happens on some accounts and is [librespot issue #1649](https://github.com/librespot-org/librespot/issues/1649). spotifyd has no fix for it.

## Keys

- `←` and `→` switch tabs.
- `↑` and `↓` move in the focused list.
- `tab` moves between the playlists and the songs of the selected playlist.
- `enter` plays the selected playlist from the start, or the selected song.
- `/` filters the playlists.
- `r` reloads the songs of the selected playlist.

Spotify may only list the songs of playlists you own or collaborate on, depending on the mode of the Spotify app. Other playlists still play with `enter`.

## Debugging

Set `DEBUG=1` to write a `debug.log` file. `make debug` starts the app under Delve on port 2345.

## Spotify app settings

spot-go uses the client ID in `internal/spotify/auth.go`. The Spotify app behind it must have this exact redirect URI registered in the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard):

```
http://127.0.0.1:10888/callback
```

Apps in development mode only work for up to 5 users added to the app's user list, and the app owner needs Spotify Premium.

## License

spot-go's source code is MIT licensed. spot-go never ships Soloist or spotifyd:

- Soloist is proprietary software from Spotify. spot-go downloads it from Spotify's servers on your computer.
- spotifyd is GPL-3.0 licensed. spot-go never links it, it only runs the spotifyd binary you installed or that spot-go downloaded from the official release.

The [project logo](assets/logo.png) is an adaptation of the [Go Gopher](https://go.dev/blog/gopher), originally designed by Renée French, and is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). The adaptation was created with AI assistance and adds headphones, a green-and-black treble clef, and a transparent background.
