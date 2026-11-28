package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

func TestScrollPageReuseRecorded(t *testing.T) {
	root := os.Getenv("YOUKAI_SCROLL_PAGE_REPLAY")
	if root == "" {
		t.Skip("private same-window replay")
	}
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	read := func(index int) platform.Frame {
		t.Helper()
		name := filepath.Join(root, fmt.Sprintf("image%03d.png", index))
		return offlineLoadedFrame(t, name, uint64(index+1), l)
	}
	var direct, cached time.Duration
	var displaced, stationary, other int
	for i := 0; i <= 104; i += 4 {
		before, f, after := read(i), read(i+3), read(i+4)
		s := time.Now()
		frames := [][2]platform.Frame{{before, f}, {before, after}, {f, after}, {after, after}}
		want := make([]ScrollMatch, len(frames))
		for j, p := range frames {
			_ = m.PixelMatcher.Match(p[1])
			want[j] = m.MatchScroll(p[0], p[1])
		}
		direct += time.Since(s)
		s = time.Now()
		cache := scrollPageCache{matcher: m.PixelMatcher}
		got := make([]ScrollMatch, len(frames))
		for j, p := range frames {
			_ = cache.match(p[1])
			got[j] = m.matchScrollWithPage(p[0], p[1], cache.match)
		}
		cached += time.Since(s)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("scroll evidence changed at view %d", i)
		}
		for _, v := range got {
			switch {
			case v.Displaced:
				displaced++
			case v.Stationary:
				stationary++
			default:
				other++
			}
		}
	}
	t.Logf("SCROLL_REPLAY displaced=%d stationary=%d other=%d original_ms=%.1f reuse_ms=%.1f", displaced, stationary, other, float64(direct)/1e6, float64(cached)/1e6)
}
