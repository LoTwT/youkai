package collect

import "github.com/LoTwT/youkai/internal/platform"

type scrollPageSample struct {
	frame platform.Frame
	page  PageMatch
}

// scrollPageCache only holds the last few exact frames of one wheel
// action. The scroll controller still processes every frame separately.
type scrollPageCache struct {
	matcher *PixelMatcher
	entries [3]scrollPageSample
	next    int
}

func (c *scrollPageCache) match(f platform.Frame) PageMatch {
	if f.ID == 0 || f.Image == nil {
		return c.matcher.Match(f)
	}
	for _, sample := range c.entries {
		if sample.frame == f {
			return sample.page
		}
	}
	m := c.matcher.Match(f)
	c.entries[c.next] = scrollPageSample{frame: f, page: m}
	c.next = (c.next + 1) % len(c.entries)
	return m
}
