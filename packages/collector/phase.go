package collector

// Phase identifies which part of a round a tick belongs to.
//
// Note that buy-ability is deliberately NOT a phase: the buy window
// (mp_buytime) spans all of freezetime and the first few seconds of live play,
// so it cuts across PhaseFreeze and PhaseLive. It is derivable from
// GameState().Rules().ConVars() if ever needed.
type Phase uint8

const (
	// PhaseFreeze is freezetime: players are frozen at spawn and can buy.
	PhaseFreeze Phase = iota
	// PhaseLive is live play, from RoundFreezetimeEnd to RoundEnd.
	PhaseLive
	// PhasePostRound is after the win condition, before RoundEndOfficial.
	// Players can still move and kill; this data is real but analytically noisy.
	PhasePostRound
)

func (p Phase) String() string {
	switch p {
	case PhaseFreeze:
		return "freeze"
	case PhaseLive:
		return "live"
	case PhasePostRound:
		return "postround"
	default:
		return "unknown"
	}
}
