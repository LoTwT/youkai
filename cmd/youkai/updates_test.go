package main

import (
	"bytes"
	"log"
	"strconv"
	"strings"
	"testing"
)

// captureLog runs f with the standard logger writing to a buffer, and
// returns what f logged.
func captureLog(f func()) string {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	f()
	return buf.String()
}

// downloadLines counts how often the completed-download line for n bytes
// appears in the captured log.
func downloadLines(out string, n int64) int {
	return strings.Count(out, "youkai: update install: downloaded "+strconv.FormatInt(n, 10)+" bytes")
}

// TestDownloadLogOncePerCompletedDownload covers one download as MyGo
// reports it: the finished total arrives twice — progressReader reports
// once when the last bytes land (n == total) and once more on EOF — and a
// report past the total changes nothing. The line still appears once.
func TestDownloadLogOncePerCompletedDownload(t *testing.T) {
	var d downloadLog
	out := captureLog(func() {
		d.report(0, 100)
		d.report(50, 100)
		d.report(100, 100) // the last bytes arrived
		d.report(100, 100) // EOF reports the total again
		d.report(101, 100) // an overshoot is still the same download
	})
	if got := downloadLines(out, 100); got != 1 {
		t.Errorf("the completed download has %d lines, want 1:\n%s", got, out)
	}
}

// TestDownloadLogDeltaFallbackLogsBothDownloads covers the failed delta
// update: the delta's download completes (and is reported twice by MyGo),
// then the whole archive downloads with the progress starting over from
// zero. Both downloads get exactly one line, so the two sizes tell the two
// downloads apart in the log.
func TestDownloadLogDeltaFallbackLogsBothDownloads(t *testing.T) {
	var d downloadLog
	out := captureLog(func() {
		d.report(0, 300) // the delta
		d.report(300, 300)
		d.report(300, 300)
		d.report(0, 1000) // the whole update, from zero
		d.report(500, 1000)
		d.report(1000, 1000)
		d.report(1000, 1000)
	})
	for _, n := range []int64{300, 1000} {
		if got := downloadLines(out, n); got != 1 {
			t.Errorf("the download of %d bytes has %d lines, want 1:\n%s", n, got, out)
		}
	}
	if got := downloadLines(out, 300) + downloadLines(out, 1000); got != 2 {
		t.Errorf("%d download lines in total, want 2 (one per download):\n%s", got, out)
	}
}

// TestDownloadLogWithoutTotal covers reports without a known size —
// total <= 0 is no completion: nothing is logged, and the reports do not
// arm or consume the completion of a later download.
func TestDownloadLogWithoutTotal(t *testing.T) {
	var d downloadLog
	out := captureLog(func() {
		d.report(5, 0)
		d.report(5, -1)
	})
	if out != "" {
		t.Errorf("logged %q without a known total, want nothing", out)
	}
}
