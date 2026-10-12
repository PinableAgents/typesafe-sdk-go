package typesafe

import (
	"encoding/json"
	"math"
)

// NonFiniteFloat64 is an explicit request-side Python v0.7.4 compatibility value.
// NaN and infinities encode as "NaN", "Infinity", and "-Infinity"; finite values
// remain JSON numbers. Use it in map/slice/struct fields which may be non-finite.
// Ordinary float64 values still fail encoding when non-finite. No global codec
// setting, recursive rewriting, or response probability relaxation is performed.
// RawMessage, json.Number, custom marshalers, tags and cycle checks retain Go's
// encoding/json semantics. Invalid raw JSON is never repaired automatically.
type NonFiniteFloat64 float64

func (v NonFiniteFloat64) MarshalJSON() ([]byte, error) {
	switch {
	case math.IsNaN(float64(v)):
		return []byte(`"NaN"`), nil
	case math.IsInf(float64(v), 1):
		return []byte(`"Infinity"`), nil
	case math.IsInf(float64(v), -1):
		return []byte(`"-Infinity"`), nil
	default:
		return json.Marshal(float64(v))
	}
}
