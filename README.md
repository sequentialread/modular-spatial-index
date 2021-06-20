# modular-spatial-index

![animated gif showing query zone and selected curve area](https://sequentialread.com/content/images/2021/06/hilbert.gif)

For the demo that this animated gif was generated from, see: https://git.sequentialread.com/forest/modular-spatial-index-demo-opengl



modular-spatial-index is a simple spatial index adapter for key/value databases, based on https://github.com/google/hilbert.

Read amplification for range queries is aproximately like 2x-3x in terms of IOPS and bandwidth compared to a 1-dimensional query.

But that constant factor on top of your O(1) database is a low price to pay for a whole new dimension, right? It's certainly better than the naive approach.

See https://sequentialread.com/building-a-spatial-index-supporting-range-query-using-space-filling-hilbert-curve
for more information.

## Interface 

```
// returns the minimum and maximum values for x and y coordinates passed into the index.
func GetValidInputRange() (int, int) { ... }

// returns two byte slices of length 8, one representing the smallest key in the index 
// and the other representing the largest possible key in the index
func GetOutputRange() ([]byte, []byte) { ... }

// Returns a slice of 8 bytes which can be used as a key in a database index,
// to be spatial-range-queried by RectangleToIndexedRanges
func GetIndexedPoint(x int, y int) ([]byte, error) { ... }


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
// if you have an extremely fast NVME SSD with a good driver, you might try 0.5 or 0.1.
// 2 is probably way too much for any modern disk to benefit from, unless your data is VERY sparse
func RectangleToIndexedRanges(x, y, width, height int, iopsCostParam float32) ([]ByteRange, error) { ... }

```



MIT license 