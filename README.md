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

1. Asks you to log in to the Spotify Web API in the browser. spot-go uses it to read your playlists, the podcasts you follow, the artists you follow, the albums you saved, what you played recently and where you stopped in each episode, and to control playback.
2. Downloads Soloist. On macOS it only installs a binary signed by Spotify.
3. Asks for a Soloist API key. Create one at the [Soloist dashboard](https://developer.spotify.com/dashboard/soloist) and paste it. spot-go saves it only on your computer, in its config folder.
4. Shows the device name, `spot-go@<hostname>`. Open Spotify on a phone or computer on the same network, open the list of devices and select it. Soloist remembers the login, so you only do this once.

Each Soloist build stops working 90 days after it was built. spot-go downloads a newer one when the current build has less than 14 days left.

The API key is passed to Soloist on its command line, so other users of the same computer can see it in `ps`.

## Keys

- `←` and `→` switch tabs.
- `↑` and `↓` move in the focused list.
- `tab` moves between the list on the left and the list on the right: the songs of the selected playlist or album, the episodes of the selected podcast, or the albums of the selected artist.
- `enter` plays the selected playlist or album from the start, the newest episode of the selected podcast, the selected artist, or the selected song, episode or album.
- `/` filters the list on the left. The matches keep the order of the list. On the Search tab, `/` types a search (see [Search](#search)).
- `r` reloads the list on the right.
- `s` changes the order of the list on the left: playlists (see [Playlist order](#playlist-order)), podcasts (see [Podcasts](#podcasts)), artists (see [Artists](#artists)) or albums (see [Albums](#albums)).
- `space` or `k` pauses what Spotify is playing, or resumes it on the device it was paused on, whether that is spot-go or another device. When Spotify has no active device, it resumes on spot-go.
- `+` (or `=`) and `-` raise and lower the Spotify volume by 5% on the device Spotify is playing on, whether that is spot-go or another device. They don't change the volume of the computer. The new volume, or why it couldn't change, shows next to the status for 3 seconds. Some devices, such as some TVs and speakers, don't let Spotify change their volume.

The status under the list shows what Spotify is playing on any device: ⏸ while it plays and ▶ while it is paused, the song or episode with its artist or podcast, a progress bar and the elapsed and total time. The time counts every second and follows Spotify every 5 seconds.

On macOS, the play/pause, next and previous keys of the keyboard, and the Now Playing controls in Control Center, work too, even when the terminal is in the background. spot-go shows macOS what Spotify is playing, so it takes over these keys while Spotify plays. If another app, such as Music, plays after it, the keys go to that app until Spotify plays again.

On Linux, spot-go shows up as an MPRIS media player on the D-Bus session bus, so the media keys of desktops such as GNOME and KDE, their media controls and `playerctl` work too. Desktops usually send the keys to the player that played last.

Spotify may only list the songs of playlists you own or collaborate on, depending on the mode of the Spotify app. Other playlists still play with `enter`.

## Search

The Search tab searches the Spotify catalog. Press `/`, type what you want to listen to and press `enter`. While you type, every key goes to the search, so `space`, `k`, `+`, `-`, `←` and `→` don't control Spotify or switch tabs. `esc` goes back to the results without searching.

The left side lists the kinds of results: all, songs, artists, albums, playlists, podcasts and episodes. Next to each kind is how many Spotify found, and next to All is how many results it mixes. The right side lists the results of the selected kind, in Spotify's order. `tab` moves between the two sides, and `r` searches again. The Web API returns at most 10 results of each kind per request, so spot-go loads more as you move down the list, up to the first 1000.

All comes first and mixes the kinds, as in the Spotify app, with the kind of each result next to it. The Web API has no mixed search, so spot-go builds it from the first 10 results of each kind:

- The top result comes first: the artist or podcast named exactly as your search, ignoring case and accents, or else the first artist, or else the first podcast.
- Then the albums, songs and playlists, taking the next one of each kind in turn: the first album, the first song, the first playlist, the second album, and so on.
- Then the podcasts and episodes the same way. When the top result is a podcast, they come before the music.

All doesn't load more results. To see more of a kind, select that kind.

`enter` plays the selected song or episode, the selected album or playlist from the start, or the selected artist, and Spotify picks the songs. On a podcast, it plays the newest episode, from where you stopped. An episode also plays from where you stopped.

## Playlist order

`s` switches the playlists between the orders of the Spotify app: Recents, Alphabetical, Creator and Custom order. spot-go remembers your choice.

Custom order is the order the Spotify Web API returns the playlists in. The Web API doesn't give the Recents order of the Spotify app, so spot-go builds its own: the most recently played or added playlists come first. spot-go learns what you played from:

- the last 50 songs played on any device, read when spot-go starts
- what plays on any device while spot-go is open, checked every 5 seconds and when the current song or episode ends
- the playlists you play in spot-go

From the second start on, a playlist counts as added when it shows up in your library, so a playlist you follow on your phone moves to the top. On the first start spot-go doesn't know when your playlists were added, so only what you played counts. Playlists spot-go never saw played or added come last, in Custom order.

spot-go saves the order and this history in `library.json`, in its config folder. Reading the songs you played needs a Spotify permission that older versions of spot-go didn't ask for, so after updating spot-go asks you to log in again.

## Podcasts

The Podcasts tab lists the shows you follow and the episodes of the selected show, newest first. `✓` marks the episodes you finished, and `31m left` shows how much is left of an episode you started. `enter` plays an episode from where you stopped, even if you stopped on another device. spot-go loads 50 episodes at a time, and more as you move down the list.

`s` switches the podcasts between Recents, Recently added and Alphabetical. Recently added is the order you followed the shows in. Recents also counts the shows you played in spot-go, or on any device while spot-go was open. The Web API doesn't say who publishes a show, so there is no Creator order. spot-go saves the podcast order and history in `library.json` too.

Reading the podcasts you follow and where you stopped in each episode needs two Spotify permissions that older versions of spot-go didn't ask for, so after updating spot-go asks you to log in again.

## Artists

The Artists tab lists the artists you follow and the albums, singles and compilations of the selected artist, in the order Spotify returns them. The Web API returns at most 10 albums per request, so spot-go loads more as you move down the list. `enter` on an artist plays the artist, and Spotify picks the songs. `enter` on an album plays it from the start. The Web API no longer gives the top songs of an artist, so spot-go can't list them.

`s` switches the artists between Recents, Recently added and Alphabetical. The Web API doesn't say when you followed an artist, so spot-go learns it: after the first time the Artists tab loads, an artist counts as added when it shows up in your library. Recents counts the artists you play in spot-go, including the albums you play from the Artists tab, and the artists played from their artist page on any device: in the last 50 songs, read when the tab opens, and while spot-go is open.

Reading the artists you follow needs a Spotify permission that older versions of spot-go didn't ask for, so after updating spot-go asks you to log in again.

## Albums

The Albums tab lists the albums you saved and the songs of the selected album. `enter` plays the album from the start, or from the selected song.

`s` switches the albums between Recents, Recently added, Alphabetical and Creator. Recently added is the order you saved the albums in, and Creator sorts them by artist. Recents also counts the albums you play in spot-go, from the Albums or the Artists tab, and the albums Spotify played on any device: in the last 50 songs, read when the tab opens, and while spot-go is open.

spot-go saves the artist and album orders and history in `library.json` too.

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
