package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
	// OR: github.com/go-gl/gl/v2.1/gl
)

const dim = 512

const rainbowCount = float64(20)
const saturationFluctuationCount = float64(8)

var frames = 0

func main() {
	run_opengl_app(func() *image.RGBA {

		seconds := float64(time.Now().UnixNano()) / float64(int64(time.Second))

		rectX := int(float64(dim) * (float64(0.4) + math.Sin(seconds*float64(1.3))*float64(0.3)))
		rectY := int(float64(dim) * (float64(0.5) + math.Cos(seconds*float64(0.3))*float64(0.2)))
		rectSize := 9 + int(float64(40)*(float64(1)+math.Sin(seconds*float64(0.843))))
		rectMaxX := rectX + rectSize
		rectMaxY := rectY + rectSize

		inputMin, inputMax := GetValidInputRange()
		_, outputMaxBytes := GetOutputRange()
		curveLength := int(binary.BigEndian.Uint64(outputMaxBytes))
		//log.Printf("inputMin: %d, inputMax: %d, curveLength: %d", inputMin, inputMax, curveLength)

		remappedRectXMin := int(lerp(float64(inputMin), float64(inputMax), float64(rectX)/float64(dim)))
		remappedRectYMin := int(lerp(float64(inputMin), float64(inputMax), float64(rectY)/float64(dim)))
		remappedRectXMax := int(lerp(float64(inputMin), float64(inputMax), float64(rectX+rectSize)/float64(dim)))
		remappedRectSize := remappedRectXMax - remappedRectXMin

		byteRanges, err := RectangleToIndexedRanges(remappedRectXMin, remappedRectYMin, remappedRectSize, remappedRectSize, 1)
		if err != nil {
			panic(err)
		}
		ranges := make([][]int, len(byteRanges))
		// log.Println("------------")
		for i, byteRange := range byteRanges {
			ranges[i] = []int{
				int(binary.BigEndian.Uint64(byteRange.Start)),
				int(binary.BigEndian.Uint64(byteRange.End)),
			}
			// log.Printf("Start: %x\n", byteRange.Start)
			// log.Printf("  End: %x\n", byteRange.End)
			// log.Printf("  Max: %x\n", outputMaxBytes)
		}
		// log.Println("------------")

		// outBytes, _ := json.MarshalIndent(ranges, "", "  ")
		// log.Println("outBytes: ", string(outBytes))

		rgba := image.NewRGBA(image.Rectangle{Min: image.Point{0, 0}, Max: image.Point{dim, dim}})
		queriedArea := 0
		for x := 0; x < dim; x++ {
			for y := 0; y < dim; y++ {

				onVertical := (x == rectMaxX || x == rectX) && y >= rectY && y <= rectMaxY
				onHorizontal := (y == rectMaxY || y == rectY) && x >= rectX && x <= rectMaxX
				if onVertical || onHorizontal {
					rgba.Set(x, y, color.White)
					continue
				}

				remappedX := int(lerp(float64(inputMin), float64(inputMax), float64(x)/float64(dim)))
				remappedY := int(lerp(float64(inputMin), float64(inputMax), float64(y)/float64(dim)))
				if y > dim-20 {
					found := false

					xOnCurveNumberLine := int(lerp(float64(0), float64(curveLength), float64(x)/float64(dim)))
					for _, curveRange := range ranges {
						if xOnCurveNumberLine >= curveRange[0] && xOnCurveNumberLine <= curveRange[1] {
							found = true
						}
					}

					if found {
						rgba.Set(x, y, color.White)
					} else {
						rgba.Set(x, y, color.Black)
					}
					continue
				}

				curvePointBytes, err := GetIndexedPoint(remappedX, remappedY)
				curvePoint := int(binary.BigEndian.Uint64(curvePointBytes))
				if err != nil {
					panic(err)
				}
				// if x*2 == y && !logged {
				// 	log.Printf("[%d,%d]: %d %d", x, y, curvePoint, int((float64(curvePoint)/float64(myCurve.N*myCurve.N))*1000))
				// }

				curveFloat := (float64(curvePoint) / float64(math.MaxInt64))
				//sat := (float64(2) + math.Sin(curveFloat*math.Pi*2*saturationFluctuationCount)) * float64(0.3333333)
				sat := 0.2
				// if curvePoint >= curvePoints[0] && curvePoint <= curvePoints[len(curvePoints)-1] {
				// 	sat = 1
				// }
				for _, rng := range ranges {
					if curvePoint >= rng[0] && curvePoint <= rng[1] {
						sat = 1
						queriedArea++
					}
				}
				hue := int(curveFloat*rainbowCount*float64(3600)) % 3600
				rainbow := hsvColor(float64(hue)*0.1, sat, sat)
				// uvColor := color.RGBA{
				// 	uint8((float32(x) / float32(width)) * float32(255)),
				// 	uint8((float32(y) / float32(width)) * float32(255)),
				// 	255,
				// 	255,
				// }

				rgba.Set(x, y, rainbow)
			}
		}

		if frames%10 == 0 {
			fmt.Printf("range count: %d, queriedArea: %d%%\n", len(ranges), int((float64(queriedArea)/float64(rectSize*rectSize))*float64(100)))
		}
		frames++

		return rgba
	})
}

func lerp(a, b, lerp float64) float64 {
	return a*(float64(1)-lerp) + b*lerp
}

func hsvColor(H, S, V float64) color.RGBA {
	Hp := H / 60.0
	C := V * S
	X := C * (1.0 - math.Abs(math.Mod(Hp, 2.0)-1.0))

	m := V - C
	r, g, b := 0.0, 0.0, 0.0

	switch {
	case 0.0 <= Hp && Hp < 1.0:
		r = C
		g = X
	case 1.0 <= Hp && Hp < 2.0:
		r = X
		g = C
	case 2.0 <= Hp && Hp < 3.0:
		g = C
		b = X
	case 3.0 <= Hp && Hp < 4.0:
		g = X
		b = C
	case 4.0 <= Hp && Hp < 5.0:
		r = X
		b = C
	case 5.0 <= Hp && Hp < 6.0:
		r = C
		b = X
	}

	return color.RGBA{uint8(int((m + r) * float64(255))), uint8(int((m + g) * float64(255))), uint8(int((m + b) * float64(255))), 0xff}
}
