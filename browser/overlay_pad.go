package main

// Controller voting: while a vote is open, hold View and press the D-pad.
// Up, Right, Down, Left pick options 1-4 (clockwise from the top).
const (
	padUp    = 0x0001 // XINPUT_GAMEPAD_DPAD_UP
	padDown  = 0x0002
	padLeft  = 0x0004
	padRight = 0x0008
	padView  = 0x0020 // XINPUT_GAMEPAD_BACK, the View button
)

var padOrder = [4]uint16{padUp, padRight, padDown, padLeft}

// padLabels name each option's controller input, in option order.
var padLabels = [4]string{"View+↑", "View+→", "View+↓", "View+←"}

// padChoice returns the option chosen by a change of buttons from prev to cur
// (View held, one D-pad direction newly pressed), or -1.
func padChoice(prev, cur uint16, options int) int {
	if cur&padView == 0 {
		return -1
	}
	pressed := cur &^ prev
	for i, b := range padOrder {
		if i < options && pressed&b != 0 {
			return i
		}
	}
	return -1
}
