package payload

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// The rounding here is Python's, on purpose. This package replaced
// movement_payload.py, and "the same demo gives the same payload" was the bar
// it had to clear before anything switched over - see golden_test.go. Python's
// round() rounds an exact tie to even and rounds the true binary value of a
// float (round(2.675, 2) is 2.67), and so does strconv, so formatting through
// a decimal string is the faithful port rather than a convenience.

// round2 is Python's round(x, 2).
func round2(x float64) float64 { return roundDigits(x, 2) }

// round1 is Python's round(x, 1).
func round1(x float64) float64 { return roundDigits(x, 1) }

func roundDigits(x float64, digits int) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', digits, 64), 64)
	return v
}

// whole is Python's int(round(x)): to the nearest integer, ties to even.
func whole(x float64) int { return int(math.RoundToEven(x)) }

// q2 is a collector float as the CSVs hold it: csvsink writes every float32
// to two decimals, and the Python read those. Quantizing here is what makes a
// payload built straight from a .dem identical to one built from that demo's
// CSV folder - without it, a coordinate of 12.504 rounds to 13 from memory but
// to 12 from the "12.50" on disk.
func q2(v float32) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(float64(v), 'f', 2, 32), 64)
	return f
}

// ordered is a JSON object that keeps its keys in insertion order. The
// per-round maps are keyed "0:1", "0:2", ... "0:10", and a browser iterates
// such keys in insertion order - which is round order - where encoding/json
// would sort them as strings and put round 10 before round 2.
type ordered[T any] struct {
	keys   []string
	values map[string]T
}

func newOrdered[T any]() *ordered[T] {
	return &ordered[T]{values: make(map[string]T)}
}

func (o *ordered[T]) set(key string, v T) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = v
}

func (o *ordered[T]) get(key string) (T, bool) {
	v, ok := o.values[key]
	return v, ok
}

func (o *ordered[T]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := json.Marshal(o.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
