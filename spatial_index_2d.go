package modularspatialindex

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"sort"
)

type SpatialIndex2D struct {
	hilbert
	integerBits     int
	edgeSizeBits    int
	intToEightBytes func(int) []byte
	eightBytesToInt func([]byte) int
}

// This is the only way to create an instance of SpatialIndex2D. integerBits must be 32 or 64.
// To use the int size of whatever processor you happen to be running on, like what golang itself does
// with the `int` type, you may simply pass in `bits.UintSize`.
func NewSpatialIndex2D(integerBits int) (*SpatialIndex2D, error) {
	if integerBits > bits.UintSize {
		return nil, fmt.Errorf("can't create a %d bit SpatialIndex2D on a %d bit CPU", integerBits, bits.UintSize)
	}
	if integerBits != 32 && integerBits != 64 {
		return nil, fmt.Errorf("%d bit SpatialIndex2D is not supported, please use 32 or 64 bit", integerBits)
	}

	// The edges of the hilbert curve plane must have a power-of-two size,
	// and for efficient arithmetic on the CPU, the length of the hilbert curve filling this plane
	// has to fit within the CPU architecture's `int` (32 bit versus 64 bit).
	//
	// As it is a space filling curve, length == area. So the length of the curve is equal to width*height.
	// therefore, the edge length of the plane should be the largest power of two less than
	//   - For 32 bit CPUs: sqrt(math.MaxInt32)
	//   - For 64 bit CPUs: sqrt(math.MaxInt64)

	// (because it's a square root, in general, it will have half as many bits.
	//  and since 1 bit is used by the sign (-/+) of the number, its half minus 1
	edgeSizeBits := (integerBits / 2) - 1

	toReturn := &SpatialIndex2D{
		hilbert: hilbert{
			edgeLength: 1 << edgeSizeBits,
		},
		integerBits:  integerBits,
		edgeSizeBits: edgeSizeBits,
	}

	if integerBits == 32 {
		toReturn.intToEightBytes = func(v int) []byte {
			eightBytes := make([]byte, 8)
			binary.BigEndian.PutUint32(eightBytes, uint32(v))
			return eightBytes
		}
		toReturn.eightBytesToInt = func(eightBytes []byte) int {
			return int(binary.BigEndian.Uint32(eightBytes[:4]))
		}
	} else {
		toReturn.intToEightBytes = func(v int) []byte {
			eightBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(eightBytes, uint64(v))
			return eightBytes
		}
		toReturn.eightBytesToInt = func(eightBytes []byte) int {
			return int(binary.BigEndian.Uint64(eightBytes[:8]))
		}
	}

	return toReturn, nil
}

// returns the minimum and maximum values for x and y coordinates passed into the index.
// 64-bit SpatialIndex2D: -1073741823, 1073741823
// 32-bit SpatialIndex2D: -16383, 16383
func (index *SpatialIndex2D) GetValidInputRange() (int, int) {
	halfHilbertEdgeLength := 1 << (index.edgeSizeBits - 1)
	return -halfHilbertEdgeLength + 1, halfHilbertEdgeLength - 1
}

// returns two byte slices of length 8, one representing the smallest key in the index
// and the other representing the largest possible key in the index
// returns (as hex) 0000000000000000, 4000000000000000
// 32 bit SpatialIndex2Ds always leave the last 4 bytes blank.
func (index *SpatialIndex2D) GetOutputRange() ([]byte, []byte) {
	return index.intToEightBytes(0), index.intToEightBytes(index.edgeLength * index.edgeLength)
}

// Returns a slice of 8 bytes which can be used as a key in a database index,
// to be spatial-range-queried by RectangleToIndexedRanges
func (index *SpatialIndex2D) GetIndexedPoint(x int, y int) ([]byte, error) {

	// x and y can be negative, but the hilbert curve implementation is only defined over positive integers.
	// so we have to attempt to transform x and y such that they are always positive.
	// btw, `(index.edgeLength >> 1)` is just half of the edge length of the hilbert plane.
	// so by adding (index.edgeLength >> 1) we are mapping from a -50%..50% range to a 0%..100% range.
	// pointToDistanceAlongCurve will handle any out-of-bounds inputs & return  an error for us.

	curvePoint, err := index.pointToDistanceAlongCurve(x+(index.edgeLength>>1), y+(index.edgeLength>>1))
	if err != nil {
		return nil, err
	}

	return index.intToEightBytes(curvePoint), nil
}

// inverse of GetIndexedPoint. Return [x,y] position from an 8-byte spatial index key
func (index *SpatialIndex2D) GetPositionFromIndexedPoint(indexedPoint []byte) (int, int, error) {
	if len(indexedPoint) < 8 {
		return 0, 0, errors.New("GetPositionFromIndexedPoint requires at least 8 bytes")
	}
	x, y, err := index.distanceAlongCurveToPoint(index.eightBytesToInt(indexedPoint))
	if err != nil {
		return 0, 0, err
	}
	return x - (index.edgeLength >> 1), y - (index.edgeLength >> 1), nil
}

// Use this with a range query on a database index.
type ByteRange struct {
	Start []byte
	End   []byte
}

// Returns a slice of 1 or more byte ranges (typically 1-4).
// The union of the results of database range queries over these ranges will contain AT LEAST
// all GetIndexedPoint(x,y) keys present within the rectangle defined by [x,y,width,height].
//
// The results will probably also contain records outside the rectangle, it's up to you to filter them out.
//
// iopsCostParam allows you to adjust a tradeoff between wasted I/O bandwidth and # of individual I/O operations.
// I think 1.0 is actually a very reasonable value to use for SSD & HDD
// (waste ~50% of bandwidth, save a lot of unneccessary I/O operations)
// if you have an extremely fast NVME SSD with a good driver, you might try 0.5 or 0.1, but I doubt it will make it any faster.
// 2 is probably way too much for any modern disk to benefit from, unless your data is VERY sparse
func (index *SpatialIndex2D) RectangleToIndexedRanges(x, y, width, height int, iopsCostParam float32) ([]ByteRange, error) {

	// scale the universe down (rounding in such a way that the original rectangle is never cropped)
	// until we reach a scale where sampling the hilbert curve over the entire area of the query rectangle
	// will be quick and painless for the CPU.
	reducedBits := 0
	for width*height > 128 {
		halfX := x / 2
		if halfX != 0 {
			x = halfX + (x % halfX)
		} else {
			x = 0
		}
		halfY := y / 2
		if halfY != 0 {
			y = halfY + (y % halfY)
		} else {
			y = 0
		}
		halfWidth := width / 2
		halfHeight := width / 2
		if halfWidth != 0 {
			width = halfWidth + (width % halfWidth)
		} else {
			width = 1
		}
		if halfWidth != 0 {
			width = halfWidth + (width % halfWidth)
		} else {
			width = 1
		}
		if halfHeight != 0 {
			height = halfHeight + (height % halfHeight)
		} else {
			height = 1
		}
		reducedBits++
	}
	if (index.edgeSizeBits - reducedBits) < 3 {
		return nil, fmt.Errorf("RectangleToIndexedRanges(): %d by %d rectangle is too large, unable to downsample it to a reasonable size.", width, height)
	}

	reducedHilbertPlaneEdgeLength := 1 << (index.edgeSizeBits - reducedBits)

	// I noticed that this method of reducing the detail is not always accurate.
	// (small sections along the edge of the rectangle can be missed by rouding errors)
	// so I also expanded the rectangle on all sides by 1 "pixel" at the downscaled size,
	// which seemed to eliminate about 90% of the errors.
	// The remaining errors I noticed were so minor I felt like I could ignore them.
	if reducedBits > 0 {
		if x > 0 {
			x--
		}
		if y > 0 {
			y--
		}
		if x+width < reducedHilbertPlaneEdgeLength {
			width++
		}
		if x+width < reducedHilbertPlaneEdgeLength {
			width++
		}
		if y+height < reducedHilbertPlaneEdgeLength {
			height++
		}
		if y+height < reducedHilbertPlaneEdgeLength {
			height++
		}
	}

	downsampledCurve := hilbert{edgeLength: reducedHilbertPlaneEdgeLength}
	curvePoints := make([]int, width*height)

	for i := 0; i < width; i++ {
		for j := 0; j < height; j++ {
			d, err := downsampledCurve.pointToDistanceAlongCurve(x+i+(downsampledCurve.edgeLength>>1), y+j+(downsampledCurve.edgeLength>>1))
			if err != nil {
				return nil, err
			}
			curvePoints[j*width+i] = d
		}
	}

	sort.Ints(curvePoints)

	ranges := [][]int{{curvePoints[0], curvePoints[0]}}
	for i := 1; i < len(curvePoints); i++ {
		distance := curvePoints[i] - curvePoints[i-1]
		if float32(distance) > float32(width*height)*iopsCostParam {
			ranges[len(ranges)-1][1] = curvePoints[i-1]
			ranges = append(ranges, []int{curvePoints[i], curvePoints[i]})
		}
	}
	ranges[len(ranges)-1][1] = curvePoints[len(curvePoints)-1]

	byteRanges := make([]ByteRange, len(ranges))
	for i, intRange := range ranges {

		// Here is where we scale the universe back up before returning the result.
		// shift the bits of the resulting curve points representing the beginning and ending
		// of the segments to be queried.
		//
		// Note that they are shifted twice as many bits (aka, squared)
		// because the units here are curve length / area.

		startCurvePoint := (intRange[0] << (reducedBits * 2))
		endCurvePoint := (intRange[1] << (reducedBits * 2))

		byteRanges[i] = ByteRange{
			Start: index.intToEightBytes(startCurvePoint),
			End:   index.intToEightBytes(endCurvePoint),
		}
	}

	return byteRanges, nil
}
