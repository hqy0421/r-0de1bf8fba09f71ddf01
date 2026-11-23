package edi

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/jf-tech/go-corelib/testlib"
	"github.com/stretchr/testify/assert"

	"github.com/jf-tech/omniparser/idr"
)

func TestIsErrTxSetRejected(t *testing.T) {
	assert.True(t, IsErrTxSetRejected(ErrTxSetRejected("test")))
	assert.Equal(t, "test", ErrTxSetRejected("test").Error())
	assert.False(t, IsErrTxSetRejected(ErrInvalidEDI("test")))
	assert.False(t, IsErrTxSetRejected(io.EOF))
}

func TestTxSetElemValue(t *testing.T) {
	seg := RawSeg{
		Elems: []RawSegElem{
			{ElemIndex: 0, CompIndex: 1, Data: []byte("SE")},
			{ElemIndex: 1, CompIndex: 1, Data: []byte("46")},
			{ElemIndex: 2, CompIndex: 1, Data: []byte("0001")},
			{ElemIndex: 2, CompIndex: 2, Data: []byte("x")},
		},
	}
	assert.Equal(t, "SE", txSetElemValue(seg, 0))
	assert.Equal(t, "46", txSetElemValue(seg, 1))
	assert.Equal(t, "0001", txSetElemValue(seg, 2))
	assert.Equal(t, "", txSetElemValue(seg, 3))
	assert.Equal(t, "", txSetElemValue(RawSeg{}, 1))
}

func TestTxSetDeclDefaults(t *testing.T) {
	d := &TxSetDecl{}
	assert.Equal(t, 1, d.countElemIndex())
	assert.False(t, d.controlCheckEnabled())
	d.CountElemIndex = 3
	assert.Equal(t, 3, d.countElemIndex())
	d.ControlElemIndex = testlib.IntPtr(2)
	assert.True(t, d.controlCheckEnabled())
}

func newTxSetTestReader(input string) *NonValidatingReader {
	return NewNonValidatingReader(
		strings.NewReader(input),
		&FileDecl{SegDelim: "\n", ElemDelim: "*"})
}

// trackAll reads all remaining raw segments from r, feeding each into the tracker,
// and returns the first track error (if any) and the tracker's pending rejection.
func trackAll(t *testing.T, tracker *txSetTracker, r *NonValidatingReader) (trackErr error) {
	for {
		seg, err := r.Read()
		if err == io.EOF {
			return tracker.checkEOF(r)
		}
		assert.NoError(t, err)
		if err := tracker.track(seg, r); err != nil {
			return err
		}
	}
}

func TestTxSetTracker(t *testing.T) {
	for _, test := range []struct {
		name             string
		decl             *TxSetDecl
		input            string
		expectedErr      string
		expectedErrFatal bool // ErrInvalidEDI if true, plain tracker error (none expected) otherwise
		expectedRej      string
	}{
		{
			name: "count matches, no rejection",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"D*x\n" +
				"SE*3*A\n",
			expectedErr: "",
			expectedRej: "",
		},
		{
			name: "count mismatch, rejection carries first/last segment positions",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"D*x\n" +
				"SE*2*A\n",
			expectedErr: "",
			expectedRej: "input 'test' transaction set rejected: first segment no.1 (char[1,8]), " +
				"last segment no.3 (char[12,19]): segment 'SE' declared segment count 2, but actually 3",
		},
		{
			name: "count mismatch, rejection, no control check",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE"},
			input: "ST*1\n" +
				"SE*5\n",
			expectedErr: "",
			expectedRej: "input 'test' transaction set rejected: first segment no.1 (char[1,6]), " +
				"last segment no.2 (char[6,11]): segment 'SE' declared segment count 5, but actually 2",
		},
		{
			name: "invalid declared count, rejection",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"SE*abc*A\n",
			expectedErr: "",
			expectedRej: "input 'test' transaction set rejected: first segment no.1 (char[1,8]), " +
				"last segment no.2 (char[8,17]): segment 'SE' has invalid declared segment count 'abc'",
		},
		{
			name: "control number mismatch, fatal",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"SE*2*B\n",
			expectedErr: "input 'test' at segment no.2 (char[8,15]): cannot prove transaction set " +
				"boundary is recovered: segment 'SE' control number 'B' does not match segment 'ST' " +
				"control number 'A' (transaction set started at segment no.1)",
			expectedErrFatal: true,
		},
		{
			name: "end segment missing control number, fatal",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"SE*2\n",
			expectedErr: "input 'test' at segment no.2 (char[8,13]): cannot prove transaction set " +
				"boundary is recovered: segment 'SE' control number '' does not match segment 'ST' " +
				"control number 'A' (transaction set started at segment no.1)",
			expectedErrFatal: true,
		},
		{
			name: "begin segment missing control number, fatal",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1\n" +
				"SE*2*A\n",
			expectedErr: "input 'test' at segment no.1 (char[1,6]): segment 'ST' is missing " +
				"transaction set control number at element index 2",
			expectedErrFatal: true,
		},
		{
			name:  "end segment without begin segment, fatal",
			decl:  &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "SE*2*A\n",
			expectedErr: "input 'test' at segment no.1 (char[1,8]): segment 'SE' appears without " +
				"a preceding 'ST'",
			expectedErrFatal: true,
		},
		{
			name: "new begin segment before previous transaction set terminated, fatal",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"ST*2*B\n",
			expectedErr: "input 'test' at segment no.2 (char[8,15]): segment 'ST' begins a new " +
				"transaction set but the previous one started at segment no.1 is not terminated by 'SE'",
			expectedErrFatal: true,
		},
		{
			name: "EOF before end segment, fatal",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"D*x\n",
			expectedErr: "input 'test' at segment no.3 (char[12,12]): transaction set started at " +
				"segment no.1 is not terminated by 'SE' before EOF",
			expectedErrFatal: true,
		},
		{
			name: "multiple transaction sets, all good",
			decl: &TxSetDecl{BeginSeg: "ST", EndSeg: "SE", ControlElemIndex: testlib.IntPtr(2)},
			input: "ST*1*A\n" +
				"SE*2*A\n" +
				"ST*2*B\n" +
				"D*x\n" +
				"SE*3*B\n",
			expectedErr: "",
			expectedRej: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tracker := newTxSetTracker("test", test.decl)
			r := newTxSetTestReader(test.input)
			trackErr := trackAll(t, tracker, r)
			if test.expectedErr == "" {
				assert.NoError(t, trackErr)
			} else {
				assert.Error(t, trackErr)
				assert.Equal(t, test.expectedErr, trackErr.Error())
				assert.Equal(t, test.expectedErrFatal, IsErrInvalidEDI(trackErr))
			}
			if test.expectedRej == "" {
				assert.Nil(t, tracker.rejection)
			} else {
				assert.Error(t, tracker.rejection)
				assert.Equal(t, test.expectedRej, tracker.rejection.Error())
				assert.True(t, IsErrTxSetRejected(tracker.rejection))
			}
		})
	}
}

const txSetTestDeclJSON = `
	{
		"segment_delimiter": "\n",
		"element_delimiter": "*",
		"transaction_set": {
			"begin_segment": "ST",
			"end_segment": "SE",
			"count_element_index": 1,
			"control_element_index": 2
		},
		"segment_declarations": [
			{
				"name": "ISA",
				"child_segments": [
					{
						"name": "GS",
						"child_segments": [
							{
								"name": "tx",
								"type": "segment_group",
								"is_target": true,
								"max": -1,
								"child_segments": [
									{ "name": "ST", "elements": [
										{ "name": "id", "index": 1 },
										{ "name": "ctrl", "index": 2 }
									]},
									{ "name": "D", "min": 0, "max": -1, "elements": [
										{ "name": "v", "index": 1 }
									]},
									{ "name": "SE", "elements": [
										{ "name": "count", "index": 1 },
										{ "name": "ctrl", "index": 2 }
									]}
								]
							}
						]
					},
					{ "name": "GE" }
				]
			},
			{ "name": "IEA" }
		]
	}`

func newTxSetTestEDIReader(t *testing.T, declJSON, input string) *ediReader {
	var decl FileDecl
	assert.NoError(t, json.Unmarshal([]byte(declJSON), &decl))
	reader, err := NewReader("test", strings.NewReader(input), &decl, "")
	assert.NoError(t, err)
	return reader
}

// TestReadTxSet_CountMismatchRejected_OthersContinue verifies that a transaction set
// whose SE declared segment count is wrong is isolated by its real transaction
// boundary and rejected (with first/last segment positions), while the other
// transaction sets continue to be converted.
func TestReadTxSet_CountMismatchRejected_OthersContinue(t *testing.T) {
	reader := newTxSetTestEDIReader(t, txSetTestDeclJSON,
		"ISA*hdr\n"+ //    seg 1
			"GS*grp\n"+ //     seg 2
			"ST*tx1*0001\n"+ // seg 3
			"D*a\n"+ //        seg 4
			"SE*3*0001\n"+ //  seg 5:  tx1, declared 3, actual 3 -> ok
			"ST*tx2*0002\n"+ // seg 6
			"D*b\n"+ //        seg 7
			"D*c\n"+ //        seg 8
			"SE*3*0002\n"+ //  seg 9:  tx2, declared 3, actual 4 -> rejected
			"ST*tx3*0003\n"+ // seg 10
			"D*d\n"+ //        seg 11
			"SE*3*0003\n"+ //  seg 12: tx3, declared 3, actual 3 -> ok
			"GE\n"+ //         seg 13
			"IEA\n") //        seg 14

	// tx1 converts to JSON normally.
	n, err := reader.Read()
	assert.NoError(t, err)
	assert.Equal(t,
		`{"D":{"v":"a"},"SE":{"count":"3","ctrl":"0001"},"ST":{"ctrl":"0001","id":"tx1"}}`,
		idr.JSONify2(n))
	reader.Release(n)

	// tx2 is rejected: continuable error carrying the first/last segment positions.
	n, err = reader.Read()
	assert.Nil(t, n)
	assert.Error(t, err)
	assert.True(t, IsErrTxSetRejected(err))
	assert.True(t, reader.IsContinuableError(err))
	assert.Equal(t,
		"input 'test' transaction set rejected: first segment no.6 (char[42,54]), "+
			"last segment no.9 (char[62,72]): segment 'SE' declared segment count 3, but actually 4",
		err.Error())

	// tx3 still converts to JSON normally.
	n, err = reader.Read()
	assert.NoError(t, err)
	assert.Equal(t,
		`{"D":{"v":"d"},"SE":{"count":"3","ctrl":"0003"},"ST":{"ctrl":"0003","id":"tx3"}}`,
		idr.JSONify2(n))
	reader.Release(n)

	n, err = reader.Read()
	assert.Equal(t, io.EOF, err)
	assert.Nil(t, n)
}

// TestReadTxSet_ConsecutiveRejections verifies the reader recovers the real
// transaction boundary repeatedly: multiple transaction sets in a row can be rejected
// (including one at the very end of the input) and reading still finishes cleanly.
func TestReadTxSet_ConsecutiveRejections(t *testing.T) {
	reader := newTxSetTestEDIReader(t, txSetTestDeclJSON,
		"ISA*hdr\n"+ //    seg 1
			"GS*grp\n"+ //     seg 2
			"ST*tx1*0001\n"+ // seg 3
			"SE*9*0001\n"+ //  seg 4:  tx1, declared 9, actual 2 -> rejected
			"ST*tx2*0002\n"+ // seg 5
			"D*a\n"+ //        seg 6
			"SE*1*0002\n"+ //  seg 7:  tx2, declared 1, actual 3 -> rejected
			"ST*tx3*0003\n"+ // seg 8
			"SE*2*0003\n"+ //  seg 9:  tx3, declared 2, actual 2 -> ok
			"GE\n"+ //         seg 10
			"IEA\n") //        seg 11

	n, err := reader.Read()
	assert.Nil(t, n)
	assert.True(t, IsErrTxSetRejected(err))
	assert.Equal(t,
		"input 'test' transaction set rejected: first segment no.3 (char[16,28]), "+
			"last segment no.4 (char[28,38]): segment 'SE' declared segment count 9, but actually 2",
		err.Error())

	n, err = reader.Read()
	assert.Nil(t, n)
	assert.True(t, IsErrTxSetRejected(err))
	assert.Equal(t,
		"input 'test' transaction set rejected: first segment no.5 (char[38,50]), "+
			"last segment no.7 (char[54,64]): segment 'SE' declared segment count 1, but actually 3",
		err.Error())

	n, err = reader.Read()
	assert.NoError(t, err)
	assert.Equal(t,
		`{"SE":{"count":"2","ctrl":"0003"},"ST":{"ctrl":"0003","id":"tx3"}}`,
		idr.JSONify2(n))
	reader.Release(n)

	n, err = reader.Read()
	assert.Equal(t, io.EOF, err)
	assert.Nil(t, n)
}

// TestReadTxSet_BoundaryRecoveryUnprovable_NoMoreOutput verifies that when the reader
// cannot prove the transaction boundary is recovered, a fatal (non-continuable) error
// is returned so no more output is produced.
func TestReadTxSet_BoundaryRecoveryUnprovable_NoMoreOutput(t *testing.T) {
	for _, test := range []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "control number mismatch",
			input: "ISA*hdr\n" +
				"GS*grp\n" +
				"ST*tx1*0001\n" +
				"D*a\n" +
				"SE*3*0001\n" +
				"ST*tx2*0002\n" +
				"D*b\n" +
				"SE*2*9999\n" + // SE02 doesn't match ST02 of tx2
				"ST*tx3*0003\n" +
				"SE*2*0003\n" +
				"GE\n" +
				"IEA\n",
			expectedErr: "input 'test' at segment no.8 (char[58,68]): cannot prove transaction set " +
				"boundary is recovered: segment 'SE' control number '9999' does not match segment " +
				"'ST' control number '0002' (transaction set started at segment no.6)",
		},
		{
			name: "EOF before end segment",
			input: "ISA*hdr\n" +
				"GS*grp\n" +
				"ST*tx1*0001\n" +
				"D*a\n",
			expectedErr: "input 'test' at segment no.5 (char[32,32]): transaction set started at " +
				"segment no.3 is not terminated by 'SE' before EOF",
		},
		{
			name: "new transaction set begins before previous one terminated",
			input: "ISA*hdr\n" +
				"GS*grp\n" +
				"ST*tx1*0001\n" +
				"D*a\n" +
				"ST*tx2*0002\n" +
				"SE*2*0002\n" +
				"GE\n" +
				"IEA\n",
			expectedErr: "input 'test' at segment no.5 (char[32,44]): segment 'ST' begins a new " +
				"transaction set but the previous one started at segment no.3 is not terminated by 'SE'",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := newTxSetTestEDIReader(t, txSetTestDeclJSON, test.input)
			var finalErr error
			for {
				_, err := reader.Read()
				if err != nil {
					finalErr = err
					break
				}
			}
			assert.Error(t, finalErr)
			assert.Equal(t, test.expectedErr, finalErr.Error())
			assert.True(t, IsErrInvalidEDI(finalErr))
			assert.False(t, reader.IsContinuableError(finalErr))
		})
	}
}
