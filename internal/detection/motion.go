package detection

import (
	"image"
	"math"
)

const (
	motionWidth  = 160
	motionHeight = 90
)

type MotionResult struct {
	Detected bool
	Score    float64
	Bounds   image.Rectangle
}

type MotionDetector struct {
	previous []uint8
	streak   int
}

func (d *MotionDetector) Reset() {
	d.previous = nil
	d.streak = 0
}

func (d *MotionDetector) Analyze(source image.Image, zone image.Rectangle, sensitivity int) MotionResult {
	if source == nil {
		return MotionResult{}
	}
	zone = normalizedBounds(source.Bounds(), zone)
	current := resizeGray(source, zone, motionWidth, motionHeight)
	if len(d.previous) != len(current) {
		d.previous = append([]uint8(nil), current...)
		return MotionResult{}
	}
	if sensitivity < 1 {
		sensitivity = 1
	}
	if sensitivity > 100 {
		sensitivity = 100
	}

	// Correct global illumination changes before comparing pixels. This avoids
	// triggering every camera when auto-exposure or infrared mode changes.
	var currentMean, previousMean float64
	for i := range current {
		currentMean += float64(current[i])
		previousMean += float64(d.previous[i])
	}
	currentMean /= float64(len(current))
	previousMean /= float64(len(current))
	illuminationDelta := currentMean - previousMean

	pixelThreshold := 28.0 - float64(sensitivity)*0.12 // 27.9 .. 16
	changed := make([]bool, len(current))
	changedCount := 0
	minX, minY := motionWidth, motionHeight
	maxX, maxY := -1, -1
	for i := range current {
		difference := math.Abs(float64(current[i]) - float64(d.previous[i]) - illuminationDelta)
		if difference < pixelThreshold {
			continue
		}
		changed[i] = true
		changedCount++
		x := i % motionWidth
		y := i / motionWidth
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}

	// A second block-level check removes isolated rain, compression and sensor
	// noise. A block counts only when at least a quarter of its pixels changed.
	activeBlocks := 0
	totalBlocks := 0
	const block = 8
	for by := 0; by < motionHeight; by += block {
		for bx := 0; bx < motionWidth; bx += block {
			totalBlocks++
			active := 0
			pixels := 0
			for y := by; y < by+block && y < motionHeight; y++ {
				for x := bx; x < bx+block && x < motionWidth; x++ {
					pixels++
					if changed[y*motionWidth+x] {
						active++
					}
				}
			}
			if pixels > 0 && float64(active)/float64(pixels) >= 0.25 {
				activeBlocks++
			}
		}
	}

	pixelRatio := float64(changedCount) / float64(len(current))
	blockRatio := float64(activeBlocks) / float64(totalBlocks)
	score := 0.65*pixelRatio + 0.35*blockRatio
	threshold := 0.16 - float64(sensitivity)*0.00135 // 0.15865 .. 0.025
	candidate := score >= threshold && activeBlocks >= 2
	if candidate {
		d.streak++
	} else {
		d.streak = 0
		copy(d.previous, current)
	}
	// Keep the stable background while confirming a candidate. Comparing the
	// next frame only against the immediately previous frame would miss a slow
	// object whose second displacement changes just its narrow edges.
	if d.streak < 2 || maxX < minX || maxY < minY {
		return MotionResult{Score: score}
	}
	copy(d.previous, current)
	d.streak = 0

	bounds := image.Rect(
		zone.Min.X+minX*zone.Dx()/motionWidth,
		zone.Min.Y+minY*zone.Dy()/motionHeight,
		zone.Min.X+(maxX+1)*zone.Dx()/motionWidth,
		zone.Min.Y+(maxY+1)*zone.Dy()/motionHeight,
	)
	return MotionResult{Detected: true, Score: score, Bounds: bounds.Intersect(zone)}
}

func normalizedBounds(bounds, zone image.Rectangle) image.Rectangle {
	if zone.Empty() {
		return bounds
	}
	zone = zone.Intersect(bounds)
	if zone.Empty() {
		return bounds
	}
	return zone
}

func resizeGray(source image.Image, sourceBounds image.Rectangle, width, height int) []uint8 {
	out := make([]uint8, width*height)
	if sourceBounds.Empty() || width < 1 || height < 1 {
		return out
	}
	for y := 0; y < height; y++ {
		sy := sourceBounds.Min.Y + (2*y+1)*sourceBounds.Dy()/(2*height)
		if sy >= sourceBounds.Max.Y {
			sy = sourceBounds.Max.Y - 1
		}
		for x := 0; x < width; x++ {
			sx := sourceBounds.Min.X + (2*x+1)*sourceBounds.Dx()/(2*width)
			if sx >= sourceBounds.Max.X {
				sx = sourceBounds.Max.X - 1
			}
			r, g, b, _ := source.At(sx, sy).RGBA()
			gray := (299*uint64(r) + 587*uint64(g) + 114*uint64(b)) / (1000 * 257)
			out[y*width+x] = uint8(gray)
		}
	}
	return out
}
