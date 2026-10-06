package portmap

import "testing"

func TestDefaultGateway(t *testing.T) {
	gw, err := defaultGateway()
	if err != nil {
		t.Skip("no default route:", err)
	}
	if gw.To4() == nil || gw.IsUnspecified() {
		t.Fatalf("gateway %v", gw)
	}
	t.Logf("default gateway %v", gw)
}
