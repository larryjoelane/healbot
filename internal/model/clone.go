package model

// Clone returns a deep copy safe to hand to another goroutine.
func (in Incident) Clone() Incident {
	out := in
	if in.SignalIDs != nil {
		out.SignalIDs = append([]string(nil), in.SignalIDs...)
	}
	if in.Hypothesis != nil {
		h := *in.Hypothesis
		h.Evidence = append([]string(nil), in.Hypothesis.Evidence...)
		out.Hypothesis = &h
	}
	if in.Actions != nil {
		out.Actions = append([]ActionRecord(nil), in.Actions...)
	}
	if in.PullRequest != nil {
		pr := *in.PullRequest
		out.PullRequest = &pr
	}
	if in.ProposedFiles != nil {
		out.ProposedFiles = append([]ProposedFile(nil), in.ProposedFiles...)
	}
	return out
}
