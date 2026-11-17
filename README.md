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

1. Asks you to log in to the Spotify Web API in the browser. spot-go uses it to read your playlists, the songs you liked, the podcasts you follow, the artists you follow, the albums you saved, what you played recently and where you stopped in each episode, and to control playback.
2. Downloads Soloist. On macOS it only installs a binary signed by Spotify.
3. Asks for a Soloist API key. Create one at the [Soloist dashboard](https://developer.spotify.com/dashboard/soloist) and paste it. spot-go saves it only on your computer, in its config folder.
4. Shows the device name, `spot-go@<hostname>`. Open Spotify on a phone or computer on the same network, open the list of devices and select it. Soloist remembers the login, so you only do this once.

Each Soloist build stops working 90 days after it was built. spot-go downloads a newer one when the current build has less than 14 days left.

The API key is passed to Soloist on its command line, so other users of the same computer can see it in `ps`.

spot-go also starts Soloist with its local WebSocket API on `127.0.0.1`, on a port the system picks, and changes shuffle and repeat on spot-go's device through it, with `soloist ctl`: the Spotify Web API can't change them on Soloist. The API has no authentication and doesn't check where a connection comes from, so programs on the same computer, and web pages open in a browser on it, that find the port can control Soloist and see what it plays. Other computers can't reach it.

## Keys

- `←` and `→` switch tabs: Queue, Search, Playlists, Podcasts, Artists and Albums. spot-go opens on the Queue tab.
- `↑` and `↓` move in the focused list.
- `tab` moves between the list on the left and the list on the right: the songs of the selected playlist or album, the episodes of the selected podcast, or the albums of the selected artist.
- `enter` plays the selected playlist or album from the start, the newest episode of the selected podcast, the selected artist, or the selected song, episode or album. On the Queue tab, it skips to the selected song or episode (see [Queue](#queue)).
- `/` filters the list on the left. The matches keep the order of the list. On the Search tab, `/` types a search (see [Search](#search)).
- `r` reloads the list on the right.
- `s` changes the order of the list on the left: playlists (see [Playlist order](#playlist-order)), podcasts (see [Podcasts](#podcasts)), artists (see [Artists](#artists)) or albums (see [Albums](#albums)).
- `space` or `k` pauses what Spotify is playing, or resumes it on the device it was paused on, whether that is spot-go or another device. When Spotify has no active device, it resumes on spot-go.
- `n` and `p` skip to the next and the previous song or episode, like the next and previous media keys.
- `>` (or `.`) and `<` (or `,`) move 10 seconds forward and back in the song or episode, without going past its start, and stopping a second before its end, so Spotify doesn't skip to the next one. A pause stays paused.
- `+` (or `=`) and `-` raise and lower the Spotify volume by 5% on the device Spotify is playing on, whether that is spot-go or another device. They don't change the volume of the computer. Some devices, such as some TVs and speakers, don't let Spotify change their volume.
- `ctrl+s` turns shuffle on and off, and `ctrl+r` goes through the repeat modes as in the Spotify app: repeat the playlist, album or podcast, repeat the song or episode, and no repeat. On spot-go's device they go through Soloist, and on other devices through the Spotify Web API.
- `ctrl+c` or `ctrl+q` quits. While Spotify plays on spot-go, spot-go first asks whether the music keeps playing (see [Quitting](#quitting)).

These keys control Spotify on the device it is playing on, from every tab. Skips, seeks, and volume, shuffle and repeat changes pressed while Spotify is still taking a change add up, and run once it ends. A seek pressed before a skip is dropped, and so are skips and seeks still waiting when a tab starts playing something else. `space` and `k` are ignored while Spotify takes a change.

On macOS, the play/pause, next and previous keys of the keyboard, and the Now Playing controls in Control Center, work too, even when the terminal is in the background. spot-go shows macOS what Spotify is playing, so it takes over these keys while Spotify plays. If another app, such as Music, plays after it, the keys go to that app until Spotify plays again.

On Linux, spot-go shows up as an MPRIS media player on the D-Bus session bus, so the media keys of desktops such as GNOME and KDE, their media controls and `playerctl` work too. Desktops usually send the keys to the player that played last.

Spotify may only list the songs of playlists you own or collaborate on, depending on the mode of the Spotify app. Other playlists still play with `enter`.

Since November 2024 the Web API no longer gives apps in development mode, like spot-go, the playlists Spotify makes, such as Discover Weekly, Release Radar, the Daily Mixes and Spotify's editorial playlists, so they don't show in the Playlists tab.

## Liked Songs

The Playlists tab starts with Liked Songs: the songs you liked, newest first, as in the Spotify app. It stays at the top in every order. Liked Songs is often long, so spot-go loads its songs when you open the Playlists tab, not at the start. `enter` plays Liked Songs from the start, or from the selected song, and the Spotify app shows it plays from Liked Songs.

The Web API doesn't document Liked Songs as something to play, and may refuse to play it, or to start it from a song. Then spot-go plays the songs it loaded instead, from the selected one or the first one, up to 100 songs that can play. Spotify then plays them on their own, not from Liked Songs, and stops after the last one unless repeat is on.

## Player

The player at the bottom of the window shows what Spotify is playing on any device, on every tab:

```
├──────────────────────────────────────────────────────────────────────────────────────────────────┤
│ ▃▇▅ ⏸ So What — Miles Davis                                                           on Pixel 7 │
│     ━━━━━╸─────────────────────────────────────────────── 1:01 / 9:22   ⇄  ↻   🔉 ━━━━━━──── 60% │
└─ space play/pause • n/p next/previous • +/- volume • </> seek • ctrl+s shuffle • ctrl+r repeat ──┘
```

- The first row has the song or episode with its artist or podcast, after ⏸ while it plays and ▶ while it is paused. The equalizer next to it dances while Spotify plays and lies flat while it is paused. When Spotify plays on another device, its name shows on the right.
- The second row has a progress bar with the elapsed and total time. The time counts every second and follows Spotify every 5 seconds. On the right are shuffle (`⇄`) and repeat (`↻`, or `↻¹` while it repeats one song), lit while they are on, and the volume of the device, which follows the volume keys and changes made on other devices.
- The bottom line lists the keys that control Spotify. The keys that don't fit a narrow window are left out.

A change made with these keys, such as a new repeat mode, or why the volume or the position couldn't change, shows on the right of the first row for 3 seconds. While a tab starts something, the first row says so, and if it fails, why.

In windows shorter than 16 lines, the player shrinks to the bottom line of the frame: the song or episode with a shorter progress bar, shuffle, repeat and the volume. The keys that control Spotify then show, as many as fit, under the list on the right, with the keys of the tab.

## Quitting

When you quit while Spotify plays on spot-go's device, spot-go asks whether the music keeps playing after you quit:

- **Keep playing** (`k`) quits and leaves Soloist playing in the background. The media keys and the Now Playing controls of the system keep working (see [Media keys after quitting](#media-keys-after-quitting)), and the Spotify app on your phone or computer controls it too, where it is still the `spot-go@<hostname>` device.
- **Stop** (`s`) stops Soloist and quits, as spot-go did before it asked.
- **Cancel** (`esc`) goes back to spot-go.

`←` and `→` choose too, and `enter` confirms. `ctrl+c` or `ctrl+q` again stops the music and quits. When Spotify plays nothing, is paused, or plays on another device, spot-go quits right away and stops Soloist. spot-go knows what plays from the last time it checked, every 5 seconds, and the keys you pressed since. Right after it starts, before it first checked, it asks if Spotify listed spot-go's device as the active one. Closing the terminal while the player shows also stops the music.

The next time spot-go starts, it finds the Soloist left playing and goes on with it, without stopping the music, so you can come back to what plays. If you quit before the player shows, or close the terminal then, that Soloist keeps playing as spot-go found it, and the media keys stay on it. The Soloist left playing keeps its build: when the build gets old, spot-go downloads a newer one the next time it starts Soloist. If the Soloist left playing stops working, spot-go starts a new one instead. If spot-go itself crashes or is killed, Soloist can keep playing too, and the next run finds it the same way.

Soloist writes what it does to `soloist.log` next to its binary, in spot-go's config folder (`~/Library/Application Support/spot-go/soloist` on macOS, `~/.config/spot-go/soloist` on Linux), so it can keep playing after spot-go quits. spot-go empties the file each time it starts Soloist.

### Media keys after quitting

Soloist plays the music, but it is spot-go that shows it in Now Playing and takes the media keys. So while Soloist plays with spot-go closed, spot-go runs again in the background, with no window, as `spot-go background`, and keeps doing that:

- Now Playing on macOS, and the MPRIS player on Linux, show what Spotify plays, on any device, as while spot-go runs. spot-go checks it every 5 seconds while it plays, and every 15 seconds while it is paused, or less often while it can't reach Spotify.
- The play/pause, next and previous keys, and the Now Playing controls, control Spotify on the device it plays on, as while spot-go runs. When Spotify has no active device, as a while after a pause, play resumes on Soloist.

spot-go in the background ends when the Soloist left playing stops, and when spot-go runs again, which takes over the keys. It also ends when Spotify no longer takes spot-go's login, and on Linux when the D-Bus session bus goes away, as when you log out. With no session bus, as over SSH, or on macOS when spot-go was built without cgo, there are no media keys to take, so it ends right away. To end it yourself, end `spot-go background` in Activity Monitor, or run `pkill -f 'spot-go background'`.

It writes the errors it hits, such as a key Spotify refused, to `background.log`, next to `soloist.log`. spot-go empties the file each time it starts in the background.

## Queue

The Queue tab, the first one and where spot-go opens, shows what Spotify plays on any device: the song or episode playing now on the left, and what plays next on the right, in order. The tab loads the queue when spot-go starts and whenever you open it, follows Spotify every 5 seconds while it is open, and loads it again right after a skip. `r` loads it again too.

`enter` plays the selected song or episode by skipping to it, as in the Spotify app: Spotify skips the songs before it, and the songs after it stay in the queue. The Web API can't play an item of the queue directly, so spot-go reads the queue again, to find where the selected item is now, and asks Spotify to skip once for each item before it. If the item left the queue in the meantime, nothing is skipped. The player then shows what Spotify plays: from where, such as where you stopped an episode, and whether it plays or stays paused.

The Web API only returns the first part of the queue, usually 20 songs or episodes, and doesn't say which ones you added to the queue and which ones come from the playlist, album or podcast playing, so spot-go lists them together.

## Search

The Search tab, right of Queue, searches the Spotify catalog. Press `/`, type what you want to listen to and press `enter`. While you type, every key goes to the search, so `space`, `k`, `n`, `p`, `<`, `>`, `+`, `-`, `←` and `→` don't control Spotify or switch tabs. `esc` goes back to the results without searching.

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
