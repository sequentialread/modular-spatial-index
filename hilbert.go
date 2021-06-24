// https://github.com/google/hilbert/

// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package modularspatialindex

import (
	"errors"
	"fmt"
)

type hilbert struct {
	edgeLength int
}

// NewHilbert returns a hilbert space which maps integers to and from the curve.
// edgeLength must be a power of two.
func newHilbert(edgeLength int) (*hilbert, error) {
	if edgeLength <= 0 {
		return nil, errors.New("edgeLength must be greater than zero")
	}

	// Test if power of two
	if (edgeLength & (edgeLength - 1)) != 0 {
		return nil, errors.New("edgeLength must be a power of two")
	}

	return &hilbert{
		edgeLength: edgeLength,
	}, nil
}

// Map transforms a one dimension value, t, in the range [0, edgeLength^2-1] to coordinates on the hilbert
// curve in the two-dimension space, where x and y are within [0,edgeLength-1].
func (s *hilbert) distanceAlongCurveToPoint(t int) (x, y int, err error) {
	if t < 0 || t >= s.edgeLength*s.edgeLength {
		return 0, 0, fmt.Errorf("hilbert Map(t) value is out of range: (0 < t=%d < s.edgeLength*s.edgeLength=%d)", t, s.edgeLength*s.edgeLength)
	}

	for i := int(1); i < s.edgeLength; i = i * 2 {
		rx := t&2 == 2
		ry := t&1 == 1
		if rx {
			ry = !ry
		}

		x, y = s.rotate(i, x, y, rx, ry)

		if rx {
			x = x + i
		}
		if ry {
			y = y + i
		}

		t /= 4
	}

	return
}

// MapInverse transform coordinates on hilbert curve from (x,y) to t.
func (s *hilbert) pointToDistanceAlongCurve(x, y int) (t int, err error) {
	if x < 0 || x >= s.edgeLength || y < 0 || y >= s.edgeLength {
		return 0, fmt.Errorf("hilbert MapInverse(x,y) value is out of range: x(0 < %d < s.edgeLength=%d) || y(0 < %d < s.edgeLength=%d)", x, s.edgeLength, y, s.edgeLength)
	}

	for i := s.edgeLength / 2; i > 0; i = i / 2 {
		rx := (x & i) > 0
		ry := (y & i) > 0

		var a int = 0
		if rx {
			a = 3
		}
		t += i * i * (a ^ b2i(ry))

		x, y = s.rotate(i, x, y, rx, ry)
	}

	return
}

// rotate rotates and flips the quadrant appropriately.
func (s *hilbert) rotate(edgeLength, x, y int, rx, ry bool) (int, int) {
	if !ry {
		if rx {
			x = edgeLength - 1 - x
			y = edgeLength - 1 - y
		}

		x, y = y, x
	}
	return x, y
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
