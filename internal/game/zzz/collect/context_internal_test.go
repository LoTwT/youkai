package collect

import "testing"

// The admission context must carry both equipped-agent avatars: the detail
// avatar is the identity matcher's primary evidence, the list avatar
// confirms it, and both become evidence files. The regions come from the
// calibrated layout, never from an ad-hoc crop.
func TestContextLinesIncludeAvatarCrops(t *testing.T) {
	l := MacCloudLayout()
	got := l.contextLines(5)
	if len(got) != 4 {
		t.Fatalf("context has %d regions, want 4: %+v", len(got), got)
	}
	want := []LineRegion{
		{Role: LineQualityBadge, Rect: l.QualityBadge},
		{Role: LineSelectedCell, Rect: l.BorderRect(5)},
		{Role: LineDetailAvatar, Rect: l.DetailAvatar},
		{Role: LineListAvatar, Rect: l.AvatarRect(5)},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("context[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if !l.DetailAvatar.In(l.Content) || !l.AvatarRect(5).In(l.Content) {
		t.Fatalf("avatar context regions escape the content: detail %v, list %v",
			l.DetailAvatar, l.AvatarRect(5))
	}
}
