package vote

import "testing"

const (
	ifA = "70f884d7-6869-469d-b4d2-4219627e2d83"
	ifV = "cc791b4b-054a-4653-9034-5dc13c809c54"
)

func TestThumbRefs(t *testing.T) {
	ref := MapThumbRef(ifA, ifV)
	base := thumbHost + ifA + "/" + ifV + "/images/thumbnail"
	if u := ThumbURLs(ref); len(u) != 2 || u[0] != base+".jpg" || u[1] != base+".png" {
		t.Fatalf("ref %q urls %v", ref, u)
	}
	if MapThumbRef("70F884D7-6869-469D-B4D2-4219627E2D83", ifV) != ref {
		t.Fatal("IDs not normalised")
	}
	for _, bad := range []string{"x", ifA, ifA + "/" + ifV + ".jpg", "../" + ifV, ifA + "/" + ifV + "/x", "https://evil.example/" + ifA} {
		if ValidThumbRef(bad) || ThumbURLs(bad) != nil {
			t.Fatalf("accepted ref %q", bad)
		}
	}
	if MapThumbRef("not-a-uuid", ifV) != "" {
		t.Fatal("built a ref from a bad id")
	}
}
