/* Standalone child process: no UI, capture, event posting, user data or
 * OS termination. It intentionally holds the main thread while a real
 * Carbon cleanup waits, matching MyGo's SessionEnding call topology. */
#import <Foundation/Foundation.h>
#include "bridge.h"
#include <dispatch/dispatch.h>
#include <dlfcn.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

void youkaiHotKeyFired(int64_t handle) { (void)handle; }

int main(int argc, char **argv) {
	@autoreleasepool {
		if (argc != 2 || ![NSThread isMainThread]) return 2;
		const char *mode = argv[1];
		void *ref = NULL;
		// F15 with all four modifiers; register only, never send input.
		int rc = yk_hotkey_register(113, (1<<8)|(1<<9)|(1<<11)|(1<<12), 1, 2, &ref);
		if (rc != 0) { fprintf(stderr, "fixture registration failed: %d\n", rc); return 2; }
		void (*prepare)(void) = dlsym(RTLD_DEFAULT, "yk_prepare_system_termination");
		bool late = strcmp(mode, "late-register") == 0;
		if (late && prepare) prepare();
		dispatch_semaphore_t entered = dispatch_semaphore_create(0);
		dispatch_semaphore_t done = dispatch_semaphore_create(0);
		__block int result = 99;
		dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
			dispatch_semaphore_signal(entered);
			if (strcmp(mode, "unregister") == 0) {
				result = yk_hotkey_unregister(ref, 2);
			} else if (strstr(mode, "register")) {
				void *other = NULL;
				result = yk_hotkey_register(107, (1<<8)|(1<<9)|(1<<11)|(1<<12), 2, 2, &other);
			} else if (strcmp(mode, "foreground") == 0) {
				result = yk_frontmost_pid(2);
			} else if (strcmp(mode, "activation") == 0) {
				// Its queued block never runs: no application is activated.
				result = yk_activate_pid(getpid(), 2);
			}
			dispatch_semaphore_signal(done);
		});
		dispatch_semaphore_wait(entered, DISPATCH_TIME_FOREVER);
		usleep(50000);
		NSTimeInterval start = [NSDate timeIntervalSinceReferenceDate];
		if (!late && prepare) prepare();
		long timeout = dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 3*NSEC_PER_SEC));
		double elapsed = [NSDate timeIntervalSinceReferenceDate] - start;
		int expected = strcmp(mode, "unregister") == 0 ? 0 : (strstr(mode, "register") ? -3 : -2);
		printf("mode=%s result=%d expected=%d main-blocked=%.3fs gate=%s\n", mode, result, expected, elapsed, prepare ? "present" : "absent");
		// Old code compiles and runs here but fails on the actual two-second
		// cleanup/registration timeout, never merely on a missing symbol.
		return timeout || result != expected || elapsed > 0.5 ? 1 : 0;
	}
}
