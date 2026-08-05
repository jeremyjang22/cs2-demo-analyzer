package main

import (
	"testing"

	"github.com/jeremyjang22/cs2-demo-analyzer/packages/collector"
)

// completeness() feeds directly into a "%s" format verb with no separator of
// its own; a bare "" for the complete case leaves a trailing space on every
// complete round's printed line, and the fix moves the separator inside the
// non-empty branch instead.
func TestCompletenessLeavesNoTrailingContentWhenComplete(t *testing.T) {
	r := &collector.Round{Meta: collector.RoundMeta{Complete: true}}
	if got := completeness(r); got != "" {
		t.Errorf("completeness(complete) = %q, want empty string", got)
	}
}

func TestCompletenessMarksIncompleteRounds(t *testing.T) {
	r := &collector.Round{Meta: collector.RoundMeta{Complete: false}}
	if got := completeness(r); got != "  (incomplete)" {
		t.Errorf("completeness(incomplete) = %q, want %q", got, "  (incomplete)")
	}
}
