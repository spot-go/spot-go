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

  spot-go downloads Soloist from Spotify on the first run. Soloist has no build for other platforms, such as Intel Macs, so spot-go doesn't run on them.
- On macOS, the Xcode Command Line Tools (`xcode-select --install`). spot-go uses them to build its support for the keyboard's media keys. Built with `CGO_ENABLED=0`, spot-go runs without them, but the media keys don't reach it.

## Running

```bash
go run .
```

With mise, run it with the pinned Go:

```bash
mise exec -- go run .
```

On the first run spot-go:

1. Asks you to log in to the Spotify Web API in the browser. spot-go uses it to read your playlists, the podcasts you follow, what you played recently and where you stopped in each episode, and to control playback.
2. Downloads Soloist. On macOS it only installs a binary signed by Spotify.
3. Asks for a Soloist API key. Create one at the [Soloist dashboard](https://developer.spotify.com/dashboard/soloist) and paste it. spot-go saves it only on your computer, in its config folder.
4. Shows the device name, `spot-go@<hostname>`. Open Spotify on a phone or computer on the same network, open the list of devices and select it. Soloist remembers the login, so you only do this once.

Each Soloist build stops working 90 days after it was built. spot-go downloads a newer one when the current build has less than 14 days left.

The API key is passed to Soloist on its command line, so other users of the same computer can see it in `ps`.

## Keys

- `←` and `→` switch tabs.
- `↑` and `↓` move in the focused list.
- `tab` moves between the list on the left and the songs or episodes of the selected playlist or podcast.
- `enter` plays the selected playlist from the start, the newest episode of the selected podcast, or the selected song or episode.
- `/` filters the playlists or podcasts. The matches keep the order of the list.
- `r` reloads the songs or episodes of the selected playlist or podcast.
- `s` changes the order of the playlists (see [Playlist order](#playlist-order)) or podcasts (see [Podcasts](#podcasts)).
- `space` or `k` pauses what Spotify is playing, or resumes it on the device it was paused on, whether that is spot-go or another device. When Spotify has no active device, it resumes on spot-go.

On macOS, the play/pause, next and previous keys of the keyboard, and the Now Playing controls in Control Center, work too, even when the terminal is in the background. spot-go shows macOS what Spotify is playing, so it takes over these keys while Spotify plays. If another app, such as Music, plays after it, the keys go to that app until Spotify plays again.

On Linux, spot-go shows up as an MPRIS media player on the D-Bus session bus, so the media keys of desktops such as GNOME and KDE, their media controls and `playerctl` work too. Desktops usually send the keys to the player that played last.

Spotify may only list the songs of playlists you own or collaborate on, depending on the mode of the Spotify app. Other playlists still play with `enter`.

## Playlist order

`s` switches the playlists between the orders of the Spotify app: Recents, Alphabetical, Creator and Custom order. spot-go remembers your choice.

Custom order is the order the Spotify Web API returns the playlists in. The Web API doesn't give the Recents order of the Spotify app, so spot-go builds its own: the most recently played or added playlists come first. spot-go learns what you played from:

- the last 50 songs played on any device, read when spot-go starts
- what plays on any device while spot-go is open, checked every 30 seconds and when the current song or episode ends
- the playlists you play in spot-go

From the second start on, a playlist counts as added when it shows up in your library, so a playlist you follow on your phone moves to the top. On the first start spot-go doesn't know when your playlists were added, so only what you played counts. Playlists spot-go never saw played or added come last, in Custom order.

spot-go saves the order and this history in `library.json`, in its config folder. Reading the songs you played needs a Spotify permission that older versions of spot-go didn't ask for, so after updating spot-go asks you to log in again.

## Podcasts

The Podcasts tab lists the shows you follow and the episodes of the selected show, newest first. `✓` marks the episodes you finished, and `31m left` shows how much is left of an episode you started. `enter` plays an episode from where you stopped, even if you stopped on another device. spot-go loads 50 episodes at a time, and more as you move down the list.

`s` switches the podcasts between Recents, Recently added and Alphabetical. Recently added is the order you followed the shows in. Recents also counts the shows you played in spot-go, or on any device while spot-go was open. The Web API doesn't say who publishes a show, so there is no Creator order. spot-go saves the podcast order and history in `library.json` too.

Reading the podcasts you follow and where you stopped in each episode needs two Spotify permissions that older versions of spot-go didn't ask for, so after updating spot-go asks you to log in again.

## Debugging

Set `DEBUG=1` to write a `debug.log` file. `make debug` starts the app under Delve on port 2345.

## Spotify app settings

spot-go uses the client ID in `internal/spotify/auth.go`. The Spotify app behind it must have this exact redirect URI registered in the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard):

```
http://127.0.0.1:10888/callback
```

Apps in development mode only work for up to 5 users added to the app's user list, and the app owner needs Spotify Premium.

## License

spot-go's source code is MIT licensed. spot-go never ships Soloist: it is proprietary software from Spotify, and spot-go downloads it from Spotify's servers on your computer.

The [project logo](assets/logo.png) is an adaptation of the [Go Gopher](https://go.dev/blog/gopher), originally designed by Renée French, and is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). The adaptation was created with AI assistance and adds headphones, a green-and-black treble clef, and a transparent background.
