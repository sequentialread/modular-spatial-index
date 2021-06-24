package modularspatialindex

import (
	"encoding/binary"
	"math/bits"
	"sort"
)

// The edges of the hilbert curve plane must have a power-of-two size,
// and for efficient arithmetic on the CPU, the length of the hilbert curve filling this plane
// has to fit within the CPU architecture's `int` registers (32 bit versus 64 bit).
//
// As it is a space filling curve, length == area. So the length of the curve is equal to width*height.
// therefore, the edge length of the plane should be the largest power of two less than
//   - For 32 bit CPUs: sqrt(math.MaxInt32)
//   - For 64 bit CPUs: sqrt(math.MaxInt64)

// (because it's a square root, in general, it will have half as many bits.
//  and since its the power of two which is LESS than the square root, it turns out to be half as many bits minus 1)

func getHilbertPlaneEdgeSizeBitsForCurrentProcessor() int {
	return (bits.UintSize / 2) - 1
}

// returns the minimum and maximum values for x and y coordinates passed into the index.
// NOTE this depends on how many bits your integer has, so it will be different on 32 bit vs 64 bit systems.
func GetValidInputRange() (int, int) {
	halfHilbertEdgeLength := 1 << (getHilbertPlaneEdgeSizeBitsForCurrentProcessor() - 1)
	return -halfHilbertEdgeLength + 1, halfHilbertEdgeLength - 1
}

// returns two byte slices of length 8, one representing the smallest key in the index
// and the other representing the largest possible key in the index
// NOTE this depends on how many bits your integer has, so it will be different on 32 bit vs 64 bit systems.
func GetOutputRange() ([]byte, []byte) {
	min := make([]byte, 8)
	binary.BigEndian.PutUint64(min, uint64(0))
	hilbertPlaneEdgeLength := 1 << getHilbertPlaneEdgeSizeBitsForCurrentProcessor()
	max := make([]byte, 8)
	binary.BigEndian.PutUint64(max, uint64(hilbertPlaneEdgeLength*hilbertPlaneEdgeLength))
	return min, max
}

// Returns a slice of 8 bytes which can be used as a key in a database index,
// to be spatial-range-queried by RectangleToIndexedRanges
func GetIndexedPoint(x int, y int) ([]byte, error) {

	curve := Hilbert{
		N: 1 << getHilbertPlaneEdgeSizeBitsForCurrentProcessor(),
	}

	// x and y can be negative, but the hilbert curve implementation is only defined over positive integers.
	// so we have to attempt to transform x and y such that they are always positive.
	// btw, `(index.N >> 1)` is just half of the edge length of the hilbert plane.
	// so by adding (index.N >> 1) we are mapping from a -0.5..0.5 range to a 0..1 range.
	// MapInverse will handle any out-of-bounds inputs & return ErrOutOfRange for us.

	mappedPoint, err := curve.MapInverse(x+(curve.N>>1), y+(curve.N>>1))
	if err != nil {
		return nil, err
	}

	// BigEndian puts the most significant bits first, so when the bits are used
	// by the database engine for sorting, they will be sorted properly.
	// For example, BigEndian looks like:
	// 168a07830e039b46
	// 168a0783b786e61d
	// 168a0784033846da
	// LittleEndian looks like:
	// b93c2fc17c078a16
	// 798575177d078a16
	// 26213f4c7d078a16
	toReturn := make([]byte, 8)
	binary.BigEndian.PutUint64(toReturn, uint64(mappedPoint))
	return toReturn, nil
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
func RectangleToIndexedRanges(x, y, width, height int, iopsCostParam float32) ([]ByteRange, error) {

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

	reducedHilbertPlaneEdgeLength := 1 << (getHilbertPlaneEdgeSizeBitsForCurrentProcessor() - reducedBits)

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

	curve := Hilbert{N: reducedHilbertPlaneEdgeLength}
	curvePoints := make([]int, width*height)

	for i := 0; i < width; i++ {
		for j := 0; j < height; j++ {
			p, err := curve.MapInverse(x+i+(curve.N>>1), y+j+(curve.N>>1))
			if err != nil {
				return nil, err
			}
			curvePoints[j*width+i] = p
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

		startBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(startBytes, uint64(startCurvePoint))

		endBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(endBytes, uint64(endCurvePoint))

		byteRanges[i] = ByteRange{
			Start: startBytes,
			End:   endBytes,
		}
	}

	return byteRanges, nil
}
