package collector

// Sink consumes completed rounds. Implementations must tolerate being called
// once per round in order, and must be closed to flush buffered output.
//
// This interface is the seam that keeps the output format reversible: csvsink
// writes gzipped CSV today, and a Parquet implementation later is a new package
// rather than a rewrite.
type Sink interface {
	Round(*Round) error
	Close() error
}
