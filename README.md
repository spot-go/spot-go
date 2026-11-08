# spot-go

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

1. Asks you to log in to the Spotify Web API in the browser. spot-go uses it to read your playlists and what you played recently, and to control playback.
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
- `/` filters the playlists. The matches keep the order of the list.
- `r` reloads the songs of the selected playlist.
- `s` changes the order of the playlists (see [Playlist order](#playlist-order)).

Spotify may only list the songs of playlists you own or collaborate on, depending on the mode of the Spotify app. Other playlists still play with `enter`.

## Playlist order

`s` switches the playlists between the orders of the Spotify app: Recents, Alphabetical, Creator and Custom order. spot-go remembers your choice.

Custom order is the order the Spotify Web API returns the playlists in. The Web API doesn't give the Recents order of the Spotify app, so spot-go builds its own: the most recently played or added playlists come first. spot-go learns what you played from:

- the last 50 songs played on any device, read when spot-go starts
- what plays on any device while spot-go is open, checked every 30 seconds
- the playlists you play in spot-go

From the second start on, a playlist counts as added when it shows up in your library, so a playlist you follow on your phone moves to the top. On the first start spot-go doesn't know when your playlists were added, so only what you played counts. Playlists spot-go never saw played or added come last, in Custom order.

spot-go saves the order and this history in `library.json`, in its config folder. Reading the songs you played needs a Spotify permission that older versions of spot-go didn't ask for, so after updating spot-go asks you to log in again.

## Debugging

Set `DEBUG=1` to write a `debug.log` file. `make debug` starts the app under Delve on port 2345.

## Spotify app settings

spot-go uses the client ID in `internal/spotify/auth.go`. The Spotify app behind it must have this exact redirect URI registered in the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard):

```
http://127.0.0.1:10888/callback
```

Apps in development mode only work for up to 5 users added to the app's user list, and the app owner needs Spotify Premium.

## License

spot-go is MIT licensed. spot-go never ships Soloist or spotifyd:

- Soloist is proprietary software from Spotify. spot-go downloads it from Spotify's servers on your computer.
- spotifyd is GPL-3.0 licensed. spot-go never links it, it only runs the spotifyd binary you installed or that spot-go downloaded from the official release.
