package floatwin

import "testing"

func TestDragTargetKeepsOffset(t *testing.T) {
	x, y := dragTarget(2400, 1200, 100, 200, 140, 260)
	if x != 2440 || y != 1260 {
		t.Fatalf("dragTarget = (%d,%d), want (2440,1260)", x, y)
	}
}

func TestUnionBounds(t *testing.T) {
	got := unionBounds([]rect{
		{X: 0, Y: 0, Width: 1920, Height: 1080},
		{X: 1920, Y: 0, Width: 1280, Height: 1024},
	})
	if got != (rect{X: 0, Y: 0, Width: 3200, Height: 1080}) {
		t.Fatalf("unionBounds = %+v", got)
	}
	if empty := unionBounds(nil); empty != (rect{}) {
		t.Fatalf("empty union = %+v", empty)
	}
}

func TestClampToBounds(t *testing.T) {
	bounds := rect{X: 0, Y: 0, Width: 1920, Height: 1080}
	cases := []struct {
		inX, inY int
		wantX    int
		wantY    int
	}{
		{100, 100, 100, 100},
		{-50, 900, 0, 900},
		{1900, 1060, 1920 - ballSize, 1080 - ballSize},
	}
	for _, test := range cases {
		x, y := clampToBounds(test.inX, test.inY, ballSize, ballSize, bounds)
		if x != test.wantX || y != test.wantY {
			t.Fatalf("clampToBounds(%d,%d) = (%d,%d), want (%d,%d)", test.inX, test.inY, x, y, test.wantX, test.wantY)
		}
	}
	if x, y := clampToBounds(50, 60, ballSize, ballSize, rect{}); x != 50 || y != 60 {
		t.Fatalf("clamp with empty bounds changed position: (%d,%d)", x, y)
	}
}
