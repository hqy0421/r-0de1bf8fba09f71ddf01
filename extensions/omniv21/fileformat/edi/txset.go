package edi

import (
	"fmt"
	"strconv"
	"strings"
)

// ErrTxSetRejected indicates a transaction set is rejected by the transaction set
// integrity check (e.g. the segment count declared on the end segment doesn't match
// the actual segment count). It is a continuable error: the reader has isolated the
// rejected transaction set by its real transaction boundary, and subsequent Read calls
// continue with the remaining transaction sets.
type ErrTxSetRejected string

func (e ErrTxSetRejected) Error() string { return string(e) }

// IsErrTxSetRejected checks if the `err` is of ErrTxSetRejected type.
func IsErrTxSetRejected(err error) bool {
	switch err.(type) {
	case ErrTxSetRejected:
		return true
	default:
		return false
	}
}

// txSetTracker tracks transaction sets (e.g. X12 ST..SE) of an EDI input stream by
// their real transaction boundaries, and verifies for each of them the segment count
// declared on the end segment against the actual number of segments in the transaction
// set.
type txSetTracker struct {
	inputName string
	decl      *TxSetDecl

	inTx         bool   // whether we're currently inside a transaction set.
	startSegNo   int    // the segment no. of the begin segment of the current transaction set.
	startRuneBeg int    // the begin char position of the begin segment.
	startRuneEnd int    // the end char position of the begin segment.
	segCount     int    // the actual number of segments of the current transaction set so far.
	controlNum   string // the control number of the begin segment, if control check is enabled.

	// rejection, if not nil, is the pending rejection for the transaction set whose end
	// segment was just read from the input. The reader picks it up (and clears it) once
	// the end segment is fully processed.
	rejection error
}

func newTxSetTracker(inputName string, decl *TxSetDecl) *txSetTracker {
	return &txSetTracker{inputName: inputName, decl: decl}
}

// txSetElemValue returns the raw data of the first component of the element at the
// given 1-based element index of a raw segment, or "" if not found.
func txSetElemValue(seg RawSeg, elemIndex int) string {
	for _, e := range seg.Elems {
		if e.ElemIndex == elemIndex && e.CompIndex == 1 {
			return string(e.Data)
		}
	}
	return ""
}

func (t *txSetTracker) fmtErr(r *NonValidatingReader, format string, args ...interface{}) string {
	return fmt.Sprintf("input '%s' at segment no.%d (char[%d,%d]): %s",
		t.inputName, r.SegCount(), r.RuneBegin(), r.RuneEnd(), fmt.Sprintf(format, args...))
}

// track feeds a just-read raw segment into the tracker. It returns a fatal
// ErrInvalidEDI if the transaction boundary cannot be proven/recovered; otherwise it
// records a pending (continuable) rejection if the just-completed transaction set
// fails the integrity check.
func (t *txSetTracker) track(seg RawSeg, r *NonValidatingReader) error {
	if !t.inTx {
		switch seg.Name {
		case t.decl.BeginSeg:
			t.inTx = true
			t.startSegNo = r.SegCount()
			t.startRuneBeg = r.RuneBegin()
			t.startRuneEnd = r.RuneEnd()
			t.segCount = 1
			t.controlNum = ""
			if t.decl.controlCheckEnabled() {
				t.controlNum = txSetElemValue(seg, *t.decl.ControlElemIndex)
				if t.controlNum == "" {
					return ErrInvalidEDI(t.fmtErr(r,
						"segment '%s' is missing transaction set control number at element index %d",
						t.decl.BeginSeg, *t.decl.ControlElemIndex))
				}
			}
		case t.decl.EndSeg:
			return ErrInvalidEDI(t.fmtErr(r,
				"segment '%s' appears without a preceding '%s'", t.decl.EndSeg, t.decl.BeginSeg))
		}
		return nil
	}
	if seg.Name == t.decl.BeginSeg {
		return ErrInvalidEDI(t.fmtErr(r,
			"segment '%s' begins a new transaction set but the previous one started at segment no.%d is not terminated by '%s'",
			t.decl.BeginSeg, t.startSegNo, t.decl.EndSeg))
	}
	t.segCount++
	if seg.Name != t.decl.EndSeg {
		return nil
	}
	// The end segment closes the current transaction set.
	t.inTx = false
	if t.decl.controlCheckEnabled() {
		endControlNum := txSetElemValue(seg, *t.decl.ControlElemIndex)
		if endControlNum == "" || endControlNum != t.controlNum {
			// The end segment cannot be proven to be the real trailer of the current
			// transaction set, thus the transaction boundary cannot be proven to be
			// recovered: fatal, no more output.
			return ErrInvalidEDI(t.fmtErr(r,
				"cannot prove transaction set boundary is recovered: segment '%s' control number '%s' does not match segment '%s' control number '%s' (transaction set started at segment no.%d)",
				t.decl.EndSeg, endControlNum, t.decl.BeginSeg, t.controlNum, t.startSegNo))
		}
	}
	declaredStr := strings.TrimSpace(txSetElemValue(seg, t.decl.countElemIndex()))
	declared, err := strconv.Atoi(declaredStr)
	if err != nil {
		t.rejection = t.newRejection(r,
			fmt.Sprintf("segment '%s' has invalid declared segment count '%s'",
				t.decl.EndSeg, declaredStr))
		return nil
	}
	if declared != t.segCount {
		t.rejection = t.newRejection(r,
			fmt.Sprintf("segment '%s' declared segment count %d, but actually %d",
				t.decl.EndSeg, declared, t.segCount))
	}
	return nil
}

// checkEOF is called when the input is exhausted. A transaction set that isn't
// terminated by its end segment means the transaction boundary cannot be proven to be
// recovered, thus a fatal ErrInvalidEDI.
func (t *txSetTracker) checkEOF(r *NonValidatingReader) error {
	if t.inTx {
		return ErrInvalidEDI(fmt.Sprintf(
			"input '%s' at segment no.%d (char[%d,%d]): transaction set started at segment no.%d is not terminated by '%s' before EOF",
			t.inputName, r.SegCount(), r.RuneEnd(), r.RuneEnd(), t.startSegNo, t.decl.EndSeg))
	}
	return nil
}

// newRejection creates the (continuable) rejection error for the transaction set whose
// end segment was just read from the given reader. The rejection carries the first and
// last segment positions of the rejected transaction set.
func (t *txSetTracker) newRejection(r *NonValidatingReader, reason string) error {
	return ErrTxSetRejected(fmt.Sprintf(
		"input '%s' transaction set rejected: first segment no.%d (char[%d,%d]), last segment no.%d (char[%d,%d]): %s",
		t.inputName,
		t.startSegNo, t.startRuneBeg, t.startRuneEnd,
		r.SegCount(), r.RuneBegin(), r.RuneEnd(),
		reason))
}
