package detection

import (
	"image"
	"math"
	"sort"
)

const (
	hogWindowWidth  = 64
	hogWindowHeight = 128
	hogCellSize     = 8
	hogBins         = 9
)

type PersonDetection struct {
	Bounds     image.Rectangle
	Score      float64
	Confidence float64
}

type PersonDetector struct{}

func NewPersonDetector() *PersonDetector { return &PersonDetector{} }

func (d *PersonDetector) Detect(source image.Image, zone image.Rectangle, minimumConfidence int) []PersonDetection {
	if source == nil {
		return nil
	}
	zone = normalizedBounds(source.Bounds(), zone)
	if zone.Dx() < 16 || zone.Dy() < 32 {
		return nil
	}
	if minimumConfidence < 40 {
		minimumConfidence = 40
	}
	if minimumConfidence > 95 {
		minimumConfidence = 95
	}
	threshold := confidenceToScore(minimumConfidence)

	// Keep inference bounded even when snapshots are 3 MP or 4K. The detector
	// receives at most 384 pixels of width and runs only after motion is found.
	scaleToAnalysis := math.Min(1, 384/float64(zone.Dx()))
	if float64(zone.Dy())*scaleToAnalysis < hogWindowHeight {
		scaleToAnalysis = math.Min(2, hogWindowHeight/float64(zone.Dy()))
	}
	analysisWidth := int(math.Round(float64(zone.Dx()) * scaleToAnalysis))
	analysisHeight := int(math.Round(float64(zone.Dy()) * scaleToAnalysis))
	if analysisWidth < hogWindowWidth || analysisHeight < hogWindowHeight {
		return nil
	}
	base := resizeGray(source, zone, analysisWidth, analysisHeight)

	var detections []PersonDetection
	pyramidScale := 1.0
	for level := 0; level < 8; level++ {
		width := int(float64(analysisWidth) / pyramidScale)
		height := int(float64(analysisHeight) / pyramidScale)
		if width < hogWindowWidth || height < hogWindowHeight {
			break
		}
		levelImage := base
		if level > 0 {
			levelImage = resizeGrayPixels(base, analysisWidth, analysisHeight, width, height)
		}
		levelDetections := scanHOG(levelImage, width, height, threshold)
		for _, found := range levelDetections {
			factorX := float64(zone.Dx()) / float64(width)
			factorY := float64(zone.Dy()) / float64(height)
			bounds := image.Rect(
				zone.Min.X+int(math.Round(float64(found.Bounds.Min.X)*factorX)),
				zone.Min.Y+int(math.Round(float64(found.Bounds.Min.Y)*factorY)),
				zone.Min.X+int(math.Round(float64(found.Bounds.Max.X)*factorX)),
				zone.Min.Y+int(math.Round(float64(found.Bounds.Max.Y)*factorY)),
			).Intersect(zone)
			found.Bounds = bounds
			detections = append(detections, found)
		}
		pyramidScale *= 1.2
	}
	return nonMaximumSuppression(detections, 0.45)
}

func scanHOG(pixels []uint8, width, height int, threshold float64) []PersonDetection {
	cellsX := width / hogCellSize
	cellsY := height / hogCellSize
	if cellsX < 8 || cellsY < 16 {
		return nil
	}
	histograms := make([]float64, cellsX*cellsY*hogBins)
	for y := 1; y < cellsY*hogCellSize-1; y++ {
		for x := 1; x < cellsX*hogCellSize-1; x++ {
			left := math.Sqrt(float64(pixels[y*width+x-1]) / 255)
			right := math.Sqrt(float64(pixels[y*width+x+1]) / 255)
			up := math.Sqrt(float64(pixels[(y-1)*width+x]) / 255)
			down := math.Sqrt(float64(pixels[(y+1)*width+x]) / 255)
			dx, dy := right-left, down-up
			magnitude := math.Hypot(dx, dy)
			if magnitude == 0 {
				continue
			}
			angle := math.Atan2(dy, dx) * 180 / math.Pi
			if angle < 0 {
				angle += 180
			}
			if angle >= 180 {
				angle -= 180
			}
			bin := angle / (180 / hogBins)
			lowerFloor := math.Floor(bin)
			lower := int(lowerFloor) % hogBins
			upper := (lower + 1) % hogBins
			orientationFraction := bin - lowerFloor

			// Bilinear spatial interpolation matches the Dalal-Triggs/OpenCV
			// descriptor closely enough for the embedded SVM coefficients.
			cellXF := (float64(x)+0.5)/hogCellSize - 0.5
			cellYF := (float64(y)+0.5)/hogCellSize - 0.5
			cellX0 := int(math.Floor(cellXF))
			cellY0 := int(math.Floor(cellYF))
			fractionX := cellXF - math.Floor(cellXF)
			fractionY := cellYF - math.Floor(cellYF)
			for offsetY, weightY := range []float64{1 - fractionY, fractionY} {
				cellY := cellY0 + offsetY
				if cellY < 0 || cellY >= cellsY {
					continue
				}
				for offsetX, weightX := range []float64{1 - fractionX, fractionX} {
					cellX := cellX0 + offsetX
					if cellX < 0 || cellX >= cellsX {
						continue
					}
					weight := magnitude * weightX * weightY
					base := (cellY*cellsX + cellX) * hogBins
					histograms[base+lower] += weight * (1 - orientationFraction)
					histograms[base+upper] += weight * orientationFraction
				}
			}
		}
	}

	var out []PersonDetection
	for cellY := 0; cellY <= cellsY-16; cellY++ {
		for cellX := 0; cellX <= cellsX-8; cellX++ {
			score := float64(openCVPeopleDetector[len(openCVPeopleDetector)-1])
			featureIndex := 0
			for blockX := 0; blockX < 7; blockX++ {
				for blockY := 0; blockY < 15; blockY++ {
					var block [36]float64
					index := 0
					for localX := 0; localX < 2; localX++ {
						for localY := 0; localY < 2; localY++ {
							cellBase := (((cellY + blockY + localY) * cellsX) + (cellX + blockX + localX)) * hogBins
							copy(block[index:index+hogBins], histograms[cellBase:cellBase+hogBins])
							index += hogBins
						}
					}
					normalizeL2Hys(block[:])
					for _, value := range block {
						score += value * float64(openCVPeopleDetector[featureIndex])
						featureIndex++
					}
				}
			}
			if score >= threshold {
				out = append(out, PersonDetection{
					Bounds:     image.Rect(cellX*hogCellSize, cellY*hogCellSize, cellX*hogCellSize+hogWindowWidth, cellY*hogCellSize+hogWindowHeight),
					Score:      score,
					Confidence: scoreToConfidence(score),
				})
			}
		}
	}
	return out
}

func normalizeL2Hys(values []float64) {
	const epsilon = 1e-5
	var sum float64
	for _, value := range values {
		sum += value * value
	}
	scale := 1 / math.Sqrt(sum+epsilon*epsilon)
	var clippedSum float64
	for index := range values {
		values[index] *= scale
		if values[index] > 0.2 {
			values[index] = 0.2
		}
		clippedSum += values[index] * values[index]
	}
	scale = 1 / math.Sqrt(clippedSum+epsilon*epsilon)
	for index := range values {
		values[index] *= scale
	}
}

func confidenceToScore(confidence int) float64 {
	probability := float64(confidence) / 100
	return math.Log(probability / (1 - probability))
}

func scoreToConfidence(score float64) float64 {
	return 1 / (1 + math.Exp(-score))
}

func nonMaximumSuppression(input []PersonDetection, threshold float64) []PersonDetection {
	if len(input) < 2 {
		return input
	}
	sort.Slice(input, func(i, j int) bool { return input[i].Score > input[j].Score })
	kept := make([]PersonDetection, 0, len(input))
	for _, candidate := range input {
		overlaps := false
		for _, existing := range kept {
			if intersectionOverUnion(candidate.Bounds, existing.Bounds) > threshold {
				overlaps = true
				break
			}
		}
		if !overlaps {
			kept = append(kept, candidate)
		}
	}
	return kept
}

func intersectionOverUnion(a, b image.Rectangle) float64 {
	intersection := a.Intersect(b)
	if intersection.Empty() {
		return 0
	}
	intersectionArea := float64(intersection.Dx() * intersection.Dy())
	unionArea := float64(a.Dx()*a.Dy()+b.Dx()*b.Dy()) - intersectionArea
	if unionArea <= 0 {
		return 0
	}
	return intersectionArea / unionArea
}

func resizeGrayPixels(source []uint8, sourceWidth, sourceHeight, width, height int) []uint8 {
	out := make([]uint8, width*height)
	for y := 0; y < height; y++ {
		sy := (2*y + 1) * sourceHeight / (2 * height)
		if sy >= sourceHeight {
			sy = sourceHeight - 1
		}
		for x := 0; x < width; x++ {
			sx := (2*x + 1) * sourceWidth / (2 * width)
			if sx >= sourceWidth {
				sx = sourceWidth - 1
			}
			out[y*width+x] = source[sy*sourceWidth+sx]
		}
	}
	return out
}
