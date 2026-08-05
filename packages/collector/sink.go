package collector

// Sink consumes completed rounds. Implementations must tolerate being called
// once per round in order, and must be closed to flush buffered output.
//
// This interface only covers the round-by-round data path. It is
// deliberately NOT the whole contract a real sink needs: main.go also calls
// SetMap, SetTickRate and Players directly on the concrete *csvsink.Sink it
// constructs, none of which are declared here. A future Parquet sink is free
// to be a new package rather than a rewrite of csvsink, but it is not a
// drop-in replacement behind this interface alone - it would need to satisfy
// those same three methods (or main.go would need to grow a second,
// interface-based path for them) before it could actually stand in for
// csvsink.Sink. Widening this interface to include them, so a real second
// implementation is a compile-time-checked drop-in, is the next step if that
// ever happens.
type Sink interface {
	Round(*Round) error
	Close() error
}
