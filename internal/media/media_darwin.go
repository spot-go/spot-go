//go:build darwin && cgo

package media

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework MediaPlayer

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <MediaPlayer/MediaPlayer.h>
#include <stdatomic.h>
#include <stdlib.h>

extern void mediaCommand(int code);

static atomic_bool mediaStopped;

static void mediaKeepAlive(void *info) {}

static void mediaRun(void) {
	CFRunLoopSourceContext context = {0};
	context.perform = mediaKeepAlive;
	CFRunLoopSourceRef keepAlive = CFRunLoopSourceCreate(NULL, 0, &context);
	CFRunLoopAddSource(CFRunLoopGetCurrent(), keepAlive, kCFRunLoopCommonModes);
	while (!atomic_load(&mediaStopped)) {
		@autoreleasepool {
			CFRunLoopRunInMode(kCFRunLoopDefaultMode, 1.0, false);
		}
	}
	CFRunLoopRemoveSource(CFRunLoopGetCurrent(), keepAlive, kCFRunLoopCommonModes);
	CFRelease(keepAlive);
}

static void mediaStop(void) {
	atomic_store(&mediaStopped, true);
	CFRunLoopStop(CFRunLoopGetMain());
	CFRunLoopWakeUp(CFRunLoopGetMain());
}

static void mediaHandle(MPRemoteCommand *command, int code) {
	command.enabled = YES;
	[command addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *event) {
		mediaCommand(code);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
}

static void mediaListen(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		static BOOL listening = NO;
		if (listening) {
			return;
		}
		listening = YES;
		MPRemoteCommandCenter *commands = [MPRemoteCommandCenter sharedCommandCenter];
		mediaHandle(commands.togglePlayPauseCommand, 0);
		mediaHandle(commands.playCommand, 1);
		mediaHandle(commands.pauseCommand, 2);
		mediaHandle(commands.nextTrackCommand, 3);
		mediaHandle(commands.previousTrackCommand, 4);
	});
}

// mediaArtwork is the artwork of the image in data, or nil when there is no
// image. It reads the image again only when key, its URL, changes, so Now
// Playing gets the same artwork while the same image shows. It runs on the
// main queue.
static MPMediaItemArtwork *mediaArtwork(NSString *key, NSData *data) {
	static NSString *shownKey;
	static MPMediaItemArtwork *shown;
	if (data == nil) {
		return nil;
	}
	if (shown != nil && [key isEqualToString:shownKey]) {
		return shown;
	}
	NSImage *image = [[NSImage alloc] initWithData:data];
	if (image == nil || image.size.width <= 0 || image.size.height <= 0) {
		return nil;
	}
	shown = [[MPMediaItemArtwork alloc] initWithBoundsSize:image.size requestHandler:^NSImage *(CGSize size) {
		return image;
	}];
	shownKey = key;
	return shown;
}

static void mediaPublish(const char *title, const char *artist, double duration, double elapsed, int playing, const char *artworkURL, const void *artwork, size_t artworkLength) {
	@autoreleasepool {
		NSString *titleText = [NSString stringWithUTF8String:title] ?: @"";
		NSString *artistText = [NSString stringWithUTF8String:artist] ?: @"";
		NSString *artworkKey = [NSString stringWithUTF8String:artworkURL] ?: @"";
		NSData *artworkData = artworkLength > 0 ? [NSData dataWithBytes:artwork length:artworkLength] : nil;
		NSMutableDictionary *info = [NSMutableDictionary dictionary];
		info[MPMediaItemPropertyTitle] = titleText;
		info[MPMediaItemPropertyArtist] = artistText;
		info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = @(elapsed);
		info[MPNowPlayingInfoPropertyPlaybackRate] = playing ? @1.0 : @0.0;
		if (duration > 0) {
			info[MPMediaItemPropertyPlaybackDuration] = @(duration);
		}
		dispatch_async(dispatch_get_main_queue(), ^{
			MPMediaItemArtwork *artwork = mediaArtwork(artworkKey, artworkData);
			if (artwork != nil) {
				info[MPMediaItemPropertyArtwork] = artwork;
			}
			MPNowPlayingInfoCenter *center = [MPNowPlayingInfoCenter defaultCenter];
			center.nowPlayingInfo = info;
			center.playbackState = playing ? MPNowPlayingPlaybackStatePlaying : MPNowPlayingPlaybackStatePaused;
		});
	}
}
*/
import "C"

import (
	"context"
	"runtime"
	"unsafe"
)

// nowPlaying shows what plays in Now Playing, with its artwork once it is
// fetched.
var nowPlaying = newArtworks(show)

func init() {
	runtime.LockOSThread()
}

func Main(run func()) {
	go func() {
		defer C.mediaStop()
		run()
	}()
	C.mediaRun()
}

func Listen(fn func(Command)) {
	setHandler(fn)
	C.mediaListen()
}

// Connected waits until Listen reached the media controls of the system, and
// returns a channel closed when it loses them. On macOS they are always
// there, so the channel never closes.
func Connected(context.Context) (<-chan struct{}, error) {
	return nil, nil
}

func Publish(info Info) {
	nowPlaying.publish(info)
}

// show shows info in Now Playing, with image as its artwork, or none when
// image is nil.
func show(info Info, image []byte) {
	url := ""
	var artwork unsafe.Pointer
	if len(image) > 0 {
		url, artwork = info.Artwork, unsafe.Pointer(&image[0])
	}
	title, artist, artworkURL := C.CString(info.Title), C.CString(info.Artist), C.CString(url)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(artist))
	defer C.free(unsafe.Pointer(artworkURL))
	playing := C.int(0)
	if info.Playing {
		playing = 1
	}
	// mediaPublish copies the image before it returns.
	C.mediaPublish(title, artist, C.double(info.Duration.Seconds()), C.double(info.Elapsed.Seconds()), playing, artworkURL, artwork, C.size_t(len(image)))
}
