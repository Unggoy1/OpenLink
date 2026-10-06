package main

import "testing"

func TestXInputLoads(t *testing.T) {
	if xinput == nil {
		t.Skip("XInput not available")
	}
	for i := 0; i < 4; i++ {
		if b, ok := padButtons(i); ok {
			t.Logf("controller in slot %d, buttons %#x", i, b)
		}
	}
}
