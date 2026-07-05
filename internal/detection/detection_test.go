package detection

import (
	"image"
	"image/color"
	"testing"
)

func TestMotionRequiresConsecutiveFrames(t *testing.T) {
	detector := &MotionDetector{}
	first := image.NewGray(image.Rect(0, 0, 320, 180))
	second := image.NewGray(first.Bounds())
	for y := 50; y < 150; y++ {
		for x := 100; x < 220; x++ {
			second.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	if result := detector.Analyze(first, image.Rectangle{}, 70); result.Detected {
		t.Fatal("first frame must establish the background")
	}
	if result := detector.Analyze(second, image.Rectangle{}, 70); result.Detected {
		t.Fatal("single changed frame must not trigger")
	}
	third := image.NewGray(first.Bounds())
	for y := 50; y < 150; y++ {
		for x := 105; x < 225; x++ {
			third.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	if result := detector.Analyze(third, image.Rectangle{}, 70); !result.Detected {
		t.Fatal("consecutive movement should trigger")
	}
}

func TestBlankImageHasNoPerson(t *testing.T) {
	image := image.NewGray(image.Rect(0, 0, 320, 240))
	if detections := NewPersonDetector().Detect(image, image.Bounds(), 50); len(detections) != 0 {
		t.Fatalf("blank image returned %d detections", len(detections))
	}
}
