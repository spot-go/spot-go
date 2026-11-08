//go:build darwin && cgo

package media

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework MediaPlayer

#import <Foundation/Foundation.h>
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

static void mediaPublish(const char *title, const char *artist, double duration, double elapsed, int playing) {
	@autoreleasepool {
		NSString *titleText = [NSString stringWithUTF8String:title] ?: @"";
		NSString *artistText = [NSString stringWithUTF8String:artist] ?: @"";
		NSMutableDictionary *info = [NSMutableDictionary dictionary];
		info[MPMediaItemPropertyTitle] = titleText;
		info[MPMediaItemPropertyArtist] = artistText;
		info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = @(elapsed);
		info[MPNowPlayingInfoPropertyPlaybackRate] = playing ? @1.0 : @0.0;
		if (duration > 0) {
			info[MPMediaItemPropertyPlaybackDuration] = @(duration);
		}
		dispatch_async(dispatch_get_main_queue(), ^{
			MPNowPlayingInfoCenter *center = [MPNowPlayingInfoCenter defaultCenter];
			center.nowPlayingInfo = info;
			center.playbackState = playing ? MPNowPlayingPlaybackStatePlaying : MPNowPlayingPlaybackStatePaused;
		});
	}
}
*/
import "C"

import (
	"runtime"
	"sync"
	"unsafe"
)

var (
	handlerMu sync.Mutex
	handler   func(Command)
)

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
	handlerMu.Lock()
	handler = fn
	handlerMu.Unlock()
	C.mediaListen()
}

func Publish(info Info) {
	title, artist := C.CString(info.Title), C.CString(info.Artist)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(artist))
	playing := C.int(0)
	if info.Playing {
		playing = 1
	}
	C.mediaPublish(title, artist, C.double(info.Duration.Seconds()), C.double(info.Elapsed.Seconds()), playing)
}

func dispatch(code int) {
	handlerMu.Lock()
	fn := handler
	handlerMu.Unlock()
	if fn != nil {
		go fn(Command(code))
	}
}
