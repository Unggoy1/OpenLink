package main

// The vote overlay keeps the same size relative to the game on every monitor,
// as Halo's own menus do: its design sizes (DIP) are drawn overlayBaseScale
// percent larger on a monitor overlayBaseHeight pixels high, and in proportion
// to the height elsewhere (1080p smaller, 4K larger), whatever the width
// (ultrawide or 16:9). It never draws smaller than overlayMinScale percent of
// the Windows display-scaling size, so text stays readable on small screens.
const (
	overlayBaseHeight = 1440
	overlayBaseScale  = 125 // percent at overlayBaseHeight
	overlayMinScale   = 80  // percent of the display-scaling size
)

// overlayScale returns device pixels per 1000 design units for a monitor of
// the given height in pixels and DPI (96 = 100% display scaling).
func overlayScale(height int32, dpi uint32) int {
	if dpi == 0 {
		dpi = 96
	}
	scale := int(height) * overlayBaseScale * 10 / overlayBaseHeight
	return max(scale, int(dpi)*1000/96*overlayMinScale/100)
}
