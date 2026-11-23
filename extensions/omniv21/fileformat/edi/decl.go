package edi

// FileDecl describes EDI specific schema settings for omniparser reader.
type FileDecl struct {
	SegDelim    string     `json:"segment_delimiter,omitempty"`
	ElemDelim   string     `json:"element_delimiter,omitempty"`
	CompDelim   *string    `json:"component_delimiter,omitempty"`
	RepDelim    *string    `json:"repetition_delimiter,omitempty"`
	ReleaseChar *string    `json:"release_character,omitempty"`
	IgnoreCRLF  bool       `json:"ignore_crlf,omitempty"`
	TxSet       *TxSetDecl `json:"transaction_set,omitempty"`
	SegDecls    []*SegDecl `json:"segment_declarations,omitempty"`
}

// TxSetDecl describes EDI transaction set (e.g. X12 ST..SE) integrity check settings.
// When specified, the reader tracks each transaction set in the input by its real
// transaction boundary: it counts the actual number of segments from the begin segment
// (e.g. ST) through the end segment (e.g. SE), inclusive, and compares the count
// against the segment count declared on the end segment (e.g. SE01). A transaction set
// whose declared count doesn't match the actual count is rejected (ErrTxSetRejected, a
// continuable error): the reader isolates the rejected transaction set by its real
// transaction boundary and subsequent transaction sets continue to be processed. If the
// reader cannot prove the transaction boundary is recovered (e.g. the end segment's
// control number doesn't match the begin segment's, or the end segment is missing
// altogether), a fatal ErrInvalidEDI is returned and no more output is produced.
type TxSetDecl struct {
	// BeginSeg is the name of the transaction set begin segment, e.g. 'ST'.
	BeginSeg string `json:"begin_segment,omitempty"`
	// EndSeg is the name of the transaction set end segment, e.g. 'SE'.
	EndSeg string `json:"end_segment,omitempty"`
	// CountElemIndex is the 1-based element index on the end segment that holds the
	// declared transaction set segment count (e.g. 1 for SE01). If not specified or
	// specified as 0, 1 is assumed.
	CountElemIndex int `json:"count_element_index,omitempty"`
	// ControlElemIndex, if specified, is the 1-based element index on both the begin
	// and the end segments that holds the transaction set control number (e.g. 2 for
	// ST02/SE02). When specified, the reader proves a transaction boundary is
	// recovered by requiring the begin and end segments' control numbers to match.
	ControlElemIndex *int `json:"control_element_index,omitempty"`
}

func (d *TxSetDecl) countElemIndex() int {
	if d.CountElemIndex <= 0 {
		return 1
	}
	return d.CountElemIndex
}

func (d *TxSetDecl) controlCheckEnabled() bool {
	return d.ControlElemIndex != nil && *d.ControlElemIndex > 0
}
