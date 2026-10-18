// youkai macOS bridge — ScreenCaptureKit window enumeration and
// single-window capture stream.
//
// Ownership and threading rules (T2 §2.2 Go 路径建议):
//   - The stream context (YKStreamContext) is strongly retained by the
//     native registry until yk_stream_release; the void* handed to Go
//     is a non-owning lookup token. Release happens only after
//     stopCapture completed and the frame queue barrier passed, so no
//     callback can race the free.
//   - Frames are delivered on the stream's private serial queue; the
//     delegate extracts the attachments, locks the pixel buffer, and
//     calls the exported Go callback synchronously so Go copies the
//     pixels while the buffer is locked. The callback does nothing
//     else — no OCR, saving or UI waits.
//   - The enumeration call is asynchronous; the wrapper bounds the
//     wait with a semaphore instead of waiting forever.
//   - The stream captures images only (no audio, no microphone), in
//     BGRA, with the cursor hidden.

#import "bridge.h"
#import "_cgo_export.h"

#import <CoreMedia/CMSampleBuffer.h>
#import <CoreVideo/CVPixelBuffer.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <dispatch/dispatch.h>
#import <Foundation/Foundation.h>
#import <stdio.h>
#import <stdlib.h>
#import <string.h>

@class YKStreamContext;

/* The registry owns the stream contexts; the void* handed to Go is a
 * non-owning token. */
static NSMutableArray *g_streams = nil;
static dispatch_queue_t g_registry_queue = nil;

static void yk_registry_init(void) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		g_streams = [NSMutableArray array];
		g_registry_queue =
		    dispatch_queue_create("youkai.sck.registry", DISPATCH_QUEUE_SERIAL);
	});
}

static void yk_registry_add(YKStreamContext *ctx) {
	yk_registry_init();
	dispatch_sync(g_registry_queue, ^{
		[g_streams addObject:ctx];
	});
}

static void yk_registry_remove(void *token) {
	yk_registry_init();
	dispatch_sync(g_registry_queue, ^{
		NSUInteger index = [g_streams indexOfObjectIdenticalTo:(__bridge id)token];
		if (index != NSNotFound) {
			[g_streams removeObjectAtIndex:index];
		}
	});
}

/* ---- SCShareableContent enumeration ---- */

static NSError *yk_enumeration_timeout_error(void) {
	return [NSError errorWithDomain:@"youkai.macos"
	                           code:1
	                       userInfo:@{
		NSLocalizedDescriptionKey : @"SCShareableContent enumeration timed out"
	}];
}

static void yk_write_err(char *errBuf, int errBufLen, NSString *msg) {
	if (errBuf == NULL || errBufLen <= 0 || msg == nil) return;
	snprintf(errBuf, (size_t)errBufLen, "%s", msg.UTF8String);
}

// yk_fetch_content performs one bounded enumeration. The completion
// handler may run on any queue; the semaphore bounds the wait.
static SCShareableContent *yk_fetch_content(double timeoutSec, NSError **outError) {
	__block SCShareableContent *content = nil;
	__block NSError *error = nil;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	[SCShareableContent
	    getShareableContentExcludingDesktopWindows:YES
	                          onScreenWindowsOnly:NO
	                       completionHandler:^(SCShareableContent *c, NSError *e) {
		                     content = c;
		                     error = e;
		                     dispatch_semaphore_signal(done);
	                       }];
	dispatch_time_t deadline =
	    dispatch_time(DISPATCH_TIME_NOW, (int64_t)(timeoutSec * NSEC_PER_SEC));
	if (dispatch_semaphore_wait(done, deadline) != 0) {
		if (outError != NULL) *outError = yk_enumeration_timeout_error();
		return nil;
	}
	if (outError != NULL) *outError = error;
	return content;
}

static void yk_copy_window(SCWindow *w, YKWindow *out) {
	memset(out, 0, sizeof *out);
	out->windowID = (uint32_t)w.windowID;
	SCRunningApplication *app = w.owningApplication;
	if (app != nil) {
		out->pid = (int32_t)app.processID;
		if (app.bundleIdentifier != nil) {
			snprintf(out->bundleID, sizeof out->bundleID, "%s", app.bundleIdentifier.UTF8String);
		}
		if (app.applicationName != nil) {
			snprintf(out->appName, sizeof out->appName, "%s", app.applicationName.UTF8String);
		}
	}
	if (w.title != nil) {
		snprintf(out->title, sizeof out->title, "%s", w.title.UTF8String);
	}
	out->layer = (int32_t)w.windowLayer;
	out->onScreen = w.isOnScreen;
	out->active = w.isActive;
	CGRect frame = w.frame;
	out->x = frame.origin.x;
	out->y = frame.origin.y;
	out->w = frame.size.width;
	out->h = frame.size.height;
}

int yk_enumerate_windows(double timeoutSec,
                         YKWindow **outWindows, int *outWindowCount,
                         YKDisplay **outDisplays, int *outDisplayCount,
                         char *errBuf, int errBufLen) {
	*outWindows = NULL;
	*outWindowCount = 0;
	*outDisplays = NULL;
	*outDisplayCount = 0;
	NSError *error = nil;
	SCShareableContent *content = yk_fetch_content(timeoutSec, &error);
	if (content == nil) {
		NSString *msg = error != nil ? error.localizedDescription : @"no shareable content";
		yk_write_err(errBuf, errBufLen, msg);
		return error != nil ? (int)error.code : -1;
	}
	NSArray<SCWindow *> *windows = content.windows;
	NSArray<SCDisplay *> *displays = content.displays;
	if (windows.count > 0) {
		YKWindow *out = (YKWindow *)calloc(windows.count, sizeof(YKWindow));
		if (out == NULL) {
			yk_write_err(errBuf, errBufLen, @"out of memory");
			return -1;
		}
		for (NSUInteger i = 0; i < windows.count; i++) {
			yk_copy_window(windows[i], &out[i]);
		}
		*outWindows = out;
		*outWindowCount = (int)windows.count;
	}
	if (displays.count > 0) {
		YKDisplay *out = (YKDisplay *)calloc(displays.count, sizeof(YKDisplay));
		if (out == NULL) {
			free(*outWindows);
			*outWindows = NULL;
			*outWindowCount = 0;
			yk_write_err(errBuf, errBufLen, @"out of memory");
			return -1;
		}
		for (NSUInteger i = 0; i < displays.count; i++) {
			SCDisplay *d = displays[i];
			memset(&out[i], 0, sizeof out[i]);
			out[i].displayID = (uint32_t)d.displayID;
			out[i].width = (int32_t)d.width;
			out[i].height = (int32_t)d.height;
			CGRect frame = d.frame;
			out[i].x = frame.origin.x;
			out[i].y = frame.origin.y;
			out[i].w = frame.size.width;
			out[i].h = frame.size.height;
		}
		*outDisplays = out;
		*outDisplayCount = (int)displays.count;
	}
	return 0;
}

void yk_free_enumeration(YKWindow *windows, YKDisplay *displays) {
	free(windows);
	free(displays);
}

/* ---- Single-window capture stream ---- */

// YKStreamContext retains everything the stream needs and carries the
// Go registry handle the callbacks are associated with.
@interface YKStreamContext : NSObject <SCStreamDelegate, SCStreamOutput>
@property(strong) SCStream *stream;
@property(strong) SCContentFilter *filter;
@property(strong) dispatch_queue_t frameQueue;
@property int64_t handle;
@end

@implementation YKStreamContext

// stream:didStopWithError: reports an unplanned stop; the planned stop
// path reports through stopCapture's completion handler instead. The
// Go side decides which is which by whether it requested the stop.
- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
	(void)stream;
	youkaiStreamEvent(self.handle, YK_STREAM_EVENT_STOPPED,
	                  error != nil ? (int32_t)error.code : 0,
	                  error != nil ? (char *)error.localizedDescription.UTF8String : NULL);
}

// Rect attachments have been observed in two shapes: an NSValue
// wrapping a CGRect, and an NSDictionary with X/Y/Width/Height
// NSNumber entries (observed on macOS 26 in the M2 field test). Both
// are accepted; anything else counts as "no attachment" instead of
// throwing. yk_rect_shape_reported prints an unrecognized shape's
// description once per process so field runs can record it.
static bool yk_rect_shape_reported = false;
static bool yk_rect_from_attachment(CFTypeRef value, CGRect *out) {
	if (value == NULL) return false;
	id obj = (__bridge id)value;
	if ([obj isKindOfClass:[NSValue class]]) {
		NSValue *v = (NSValue *)obj;
		if (strcmp(v.objCType, @encode(CGRect)) == 0) {
			*out = v.rectValue;
			return true;
		}
		return false;
	}
	if ([obj isKindOfClass:[NSDictionary class]]) {
		NSDictionary *d = (NSDictionary *)obj;
		NSNumber *x = d[@"X"];
		NSNumber *y = d[@"Y"];
		NSNumber *w = d[@"Width"];
		NSNumber *h = d[@"Height"];
		if (x == nil || y == nil || w == nil || h == nil) {
			if (!yk_rect_shape_reported) {
				yk_rect_shape_reported = true;
				fprintf(stderr, "fieldprobe: rect attachment dictionary without X/Y/Width/Height: %s\n",
				        d.description.UTF8String);
			}
			return false;
		}
		*out = CGRectMake(x.doubleValue, y.doubleValue, w.doubleValue, h.doubleValue);
		return true;
	}
	if (!yk_rect_shape_reported) {
		yk_rect_shape_reported = true;
		fprintf(stderr, "fieldprobe: unrecognized rect attachment of class %s: %s\n",
		        object_getClassName(obj), [obj description].UTF8String);
	}
	return false;
}

// stream:didOutputSampleBuffer:ofType: extracts the frame status,
// display time and geometry attachments, then calls Go while the
// pixel buffer is locked so Go can copy the pixels.
- (void)stream:(SCStream *)stream
    didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer
                  ofType:(SCStreamOutputType)type {
	(void)stream;
	if (type != SCStreamOutputTypeScreen) return;

	YKFrameGeometry geo;
	memset(&geo, 0, sizeof geo);
	geo.status = -1;

	CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, false);
	if (attachments != NULL && CFArrayGetCount(attachments) > 0) {
		CFDictionaryRef info =
		    (CFDictionaryRef)CFArrayGetValueAtIndex(attachments, 0);
		if (info != NULL) {
			CFNumberRef status = (CFNumberRef)CFDictionaryGetValue(
			    info, (__bridge CFStringRef)SCStreamFrameInfoStatus);
			if (status != NULL) {
				long value = 0;
				if (CFNumberGetValue(status, kCFNumberLongType, &value)) {
					geo.status = (int32_t)value;
				}
			}
			CFNumberRef displayTime = (CFNumberRef)CFDictionaryGetValue(
			    info, (__bridge CFStringRef)SCStreamFrameInfoDisplayTime);
			if (displayTime != NULL) {
				int64_t value = 0;
				if (CFNumberGetValue(displayTime, kCFNumberSInt64Type, &value)) {
					geo.hasDisplayTime = true;
					geo.displayTime = (uint64_t)value;
				}
			}
			CFNumberRef contentScale = (CFNumberRef)CFDictionaryGetValue(
			    info, (__bridge CFStringRef)SCStreamFrameInfoContentScale);
			if (contentScale != NULL) {
				double value = 0;
				if (CFNumberGetValue(contentScale, kCFNumberDoubleType, &value)) {
					geo.hasContentScale = true;
					geo.contentScale = value;
				}
			}
			CFNumberRef scaleFactor = (CFNumberRef)CFDictionaryGetValue(
			    info, (__bridge CFStringRef)SCStreamFrameInfoScaleFactor);
			if (scaleFactor != NULL) {
				double value = 0;
				if (CFNumberGetValue(scaleFactor, kCFNumberDoubleType, &value)) {
					geo.hasScaleFactor = true;
					geo.scaleFactor = value;
				}
			}
			// Rect attachments must not kill the frame callback when the
			// system delivers an unexpected shape; the numbers above stay
			// valid and the rects simply count as absent.
			@try {
				CGRect r;
				if (yk_rect_from_attachment(
				        CFDictionaryGetValue(info, (__bridge CFStringRef)SCStreamFrameInfoContentRect), &r)) {
					geo.hasContentRect = true;
					geo.contentX = r.origin.x;
					geo.contentY = r.origin.y;
					geo.contentW = r.size.width;
					geo.contentH = r.size.height;
				}
				if (yk_rect_from_attachment(
				        CFDictionaryGetValue(info, (__bridge CFStringRef)SCStreamFrameInfoScreenRect), &r)) {
					geo.hasScreenRect = true;
					geo.screenX = r.origin.x;
					geo.screenY = r.origin.y;
					geo.screenW = r.size.width;
					geo.screenH = r.size.height;
				}
			} @catch (NSException *e) {
				geo.hasContentRect = false;
				geo.hasScreenRect = false;
			}
		}
	}

	CVPixelBufferRef pixels = CMSampleBufferGetImageBuffer(sampleBuffer);
	if (pixels != NULL) {
		geo.surfaceWidth = (int32_t)CVPixelBufferGetWidth(pixels);
		geo.surfaceHeight = (int32_t)CVPixelBufferGetHeight(pixels);
		geo.bytesPerRow = (int32_t)CVPixelBufferGetBytesPerRow(pixels);
		CVPixelBufferLockBaseAddress(pixels, kCVPixelBufferLock_ReadOnly);
		void *base = CVPixelBufferGetBaseAddress(pixels);
		size_t len = (size_t)geo.bytesPerRow * (size_t)geo.surfaceHeight;
		youkaiFrameArrived(self.handle, &geo, base, (int)len);
		CVPixelBufferUnlockBaseAddress(pixels, kCVPixelBufferLock_ReadOnly);
	} else {
		youkaiFrameArrived(self.handle, &geo, NULL, 0);
	}
}

@end

int yk_stream_start(uint32_t windowID, int64_t handle, void **outStream,
                    char *errBuf, int errBufLen) {
	*outStream = NULL;
	NSError *error = nil;
	SCShareableContent *content = yk_fetch_content(5.0, &error);
	if (content == nil) {
		NSString *msg = error != nil ? error.localizedDescription : @"no shareable content";
		yk_write_err(errBuf, errBufLen, [NSString stringWithFormat:@"enumerating windows: %@", msg]);
		return error != nil ? (int)error.code : -1;
	}
	SCWindow *window = nil;
	for (SCWindow *w in content.windows) {
		if ((uint32_t)w.windowID == windowID) {
			window = w;
			break;
		}
	}
	if (window == nil) {
		yk_write_err(errBuf, errBufLen,
		             [NSString stringWithFormat:@"window %u not found", windowID]);
		return -1;
	}

	SCContentFilter *filter =
	    [[SCContentFilter alloc] initWithDesktopIndependentWindow:window];
	SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
	// Image only: audio capture stays off (its default). BGRA output,
	// the cursor hidden so it never occludes OCR targets, the native
	// queue depth at its default of three.
	config.pixelFormat = kCVPixelFormatType_32BGRA;
	config.showsCursor = NO;

	YKStreamContext *ctx = [[YKStreamContext alloc] init];
	ctx.handle = handle;
	ctx.filter = filter;
	ctx.frameQueue =
	    dispatch_queue_create("youkai.sck.frames", DISPATCH_QUEUE_SERIAL);

	SCStream *stream = [[SCStream alloc] initWithFilter:filter
	                                      configuration:config
	                                           delegate:ctx];
	NSError *addError = nil;
	if (![stream addStreamOutput:ctx
	                         type:SCStreamOutputTypeScreen
	              sampleHandlerQueue:ctx.frameQueue
	                          error:&addError]) {
		NSString *msg = addError != nil ? addError.localizedDescription : @"unknown error";
		yk_write_err(errBuf, errBufLen, [NSString stringWithFormat:@"adding stream output: %@", msg]);
		return addError != nil ? (int)addError.code : -1;
	}
	ctx.stream = stream;
	yk_registry_add(ctx);

	[stream startCaptureWithCompletionHandler:^(NSError *startError) {
		youkaiStreamEvent(handle, YK_STREAM_EVENT_STARTED,
		                  startError != nil ? (int32_t)startError.code : 0,
		                  startError != nil ? (char *)startError.localizedDescription.UTF8String : NULL);
	}];

	*outStream = (__bridge void *)ctx;
	return 0;
}

int yk_stream_request_stop(void *stream) {
	if (stream == NULL) return -1;
	YKStreamContext *ctx = (__bridge YKStreamContext *)stream;
	[ctx.stream stopCaptureWithCompletionHandler:^(NSError *stopError) {
		youkaiStreamEvent(ctx.handle, YK_STREAM_EVENT_STOPPED,
		                  stopError != nil ? (int32_t)stopError.code : 0,
		                  stopError != nil ? (char *)stopError.localizedDescription.UTF8String : NULL);
	}];
	return 0;
}

int yk_stream_drain(void *stream) {
	if (stream == NULL) return -1;
	YKStreamContext *ctx = (__bridge YKStreamContext *)stream;
	// A barrier on the frame queue: it runs after every frame callback
	// already enqueued, so when Go sees the drained event no further
	// frame callback from this stream is in flight.
	dispatch_async(ctx.frameQueue, ^{
		youkaiStreamEvent(ctx.handle, YK_STREAM_EVENT_DRAINED, 0, NULL);
	});
	return 0;
}

void yk_stream_release(void *stream) {
	if (stream == NULL) return;
	YKStreamContext *ctx = (__bridge YKStreamContext *)stream;
	[ctx.stream removeStreamOutput:ctx type:SCStreamOutputTypeScreen error:NULL];
	yk_registry_remove(stream);
}
