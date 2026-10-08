package main

import "testing"

func TestOverlayScaleFollowsMonitorHeight(t *testing.T) {
	for _, c := range []struct {
		height int32
		dpi    uint32
		want   int
	}{
		{1440, 96, 1250},  // the baseline: 25% larger than the design size
		{1440, 120, 1250}, // display scaling does not change it
		{1080, 96, 937},   // 1080p: same share of the screen height
		{2160, 96, 1875},  // 4K at 100%
		{2160, 144, 1875}, // 4K at 150%
		{720, 96, 800},    // small screen: at least 80% of the display-scaling size
		{720, 144, 1200},
		{1080, 0, 937}, // unknown DPI counts as 96
	} {
		if got := overlayScale(c.height, c.dpi); got != c.want {
			t.Errorf("height %d dpi %d: %d, want %d", c.height, c.dpi, got, c.want)
		}
	}
}
