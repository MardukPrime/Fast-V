package floatwin

// rect is a screen rectangle in pixel coordinates.
type rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// dragTarget computes the window position for a pointer drag: the ball keeps the
// offset that existed between the cursor and the window when the drag started.
func dragTarget(winX, winY, grabX, grabY, currentX, currentY int) (int, int) {
	return winX + (currentX - grabX), winY + (currentY - grabY)
}

// unionBounds returns the bounding box that covers every supplied rectangle.
// Zero rectangles yield an empty rect.
func unionBounds(rects []rect) rect {
	if len(rects) == 0 {
		return rect{}
	}
	result := rects[0]
	for _, r := range rects[1:] {
		if r.X < result.X {
			result.Width += result.X - r.X
			result.X = r.X
		}
		if r.Y < result.Y {
			result.Height += result.Y - r.Y
			result.Y = r.Y
		}
		if right := r.X + r.Width; right > result.X+result.Width {
			result.Width = right - result.X
		}
		if bottom := r.Y + r.Height; bottom > result.Y+result.Height {
			result.Height = bottom - result.Y
		}
	}
	return result
}

// clampToBounds keeps a window of the given size fully inside the bounds. When
// the bounds are empty (platform reported no screens) the position is returned
// unchanged so dragging elsewhere is not broken by a missing display list.
func clampToBounds(x, y, width, height int, bounds rect) (int, int) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return x, y
	}
	maxX := bounds.X + bounds.Width - width
	maxY := bounds.Y + bounds.Height - height
	if x > maxX {
		x = maxX
	}
	if y > maxY {
		y = maxY
	}
	if x < bounds.X {
		x = bounds.X
	}
	if y < bounds.Y {
		y = bounds.Y
	}
	return x, y
}
