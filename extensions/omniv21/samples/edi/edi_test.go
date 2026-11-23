package edi

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/bradleyjkemp/cupaloy"
	"github.com/jf-tech/go-corelib/jsons"
	"github.com/stretchr/testify/assert"

	"github.com/jf-tech/omniparser"
	"github.com/jf-tech/omniparser/errs"
	"github.com/jf-tech/omniparser/extensions/omniv21/fileformat/edi"
	"github.com/jf-tech/omniparser/extensions/omniv21/samples"
	"github.com/jf-tech/omniparser/transformctx"
)

type testCase struct {
	schemaFile string
	inputFile  string
	schema     omniparser.Schema
	input      []byte
}

const (
	test1_CanadaPost_EDI_214 = iota
	test2_UPS_EDI_210
	test3_X12_834
)

var tests = []testCase{
	{
		// test1_CanadaPost_EDI_214
		schemaFile: "./1_canadapost_edi_214.schema.json",
		inputFile:  "./1_canadapost_edi_214.input.txt",
	},
	{
		// test2_UPS_EDI_210
		schemaFile: "./2_ups_edi_210.schema.json",
		inputFile:  "./2_ups_edi_210.input.txt",
	},
	{
		// test3_X12_834
		schemaFile: "./3_x12_834.schema.json",
		inputFile:  "./3_x12_834.input.txt",
	},
}

func init() {
	for i := range tests {
		schema, err := ioutil.ReadFile(tests[i].schemaFile)
		if err != nil {
			panic(err)
		}
		tests[i].schema, err = omniparser.NewSchema("bench", bytes.NewReader(schema))
		if err != nil {
			panic(err)
		}
		tests[i].input, err = ioutil.ReadFile(tests[i].inputFile)
		if err != nil {
			panic(err)
		}
	}
}

func (tst testCase) doTest(t *testing.T) {
	cupaloy.SnapshotT(t, jsons.BPJ(samples.SampleTestCommon(t, tst.schemaFile, tst.inputFile)))
}

func (tst testCase) doBenchmark(b *testing.B) {
	for i := 0; i < b.N; i++ {
		transform, err := tst.schema.NewTransform(
			"bench", bytes.NewReader(tst.input), &transformctx.Ctx{})
		if err != nil {
			b.FailNow()
		}
		for {
			_, err = transform.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.FailNow()
			}
		}
	}
}

func Test1_CanadaPost_EDI_214(t *testing.T) {
	tests[test1_CanadaPost_EDI_214].doTest(t)
}

func Test2_UPS_EDI_210(t *testing.T) {
	tests[test2_UPS_EDI_210].doTest(t)
}

func Test3_X12_834(t *testing.T) {
	tests[test3_X12_834].doTest(t)
}

// Test4_X12_210_TxSetRejection verifies that in a multi-transaction EDI input where
// one transaction set's SE declared segment count is wrong, that transaction set is
// isolated by its real transaction boundary and rejected (with the first/last segment
// positions in the rejection), while the other transaction sets continue to be
// transformed to JSON.
func Test4_X12_210_TxSetRejection(t *testing.T) {
	schemaFileReader, err := os.Open("./4_x12_210_txset_rejection.schema.json")
	assert.NoError(t, err)
	defer schemaFileReader.Close()
	inputFileReader, err := os.Open("./4_x12_210_txset_rejection.input.txt")
	assert.NoError(t, err)
	defer inputFileReader.Close()

	schema, err := omniparser.NewSchema("4_x12_210_txset_rejection.schema.json", schemaFileReader)
	assert.NoError(t, err)
	transform, err := schema.NewTransform(
		"4_x12_210_txset_rejection.input.txt", inputFileReader, &transformctx.Ctx{})
	assert.NoError(t, err)

	// Transaction set 1 (ST*210*000158669, SE declares 5, actual 5): transforms fine.
	record, err := transform.Read()
	assert.NoError(t, err)
	assert.JSONEq(t,
		`{"invoice_number": "0000001808WW308", "amount_due": "1700"}`,
		string(record))

	// Transaction set 2 (ST*210*000158670, SE declares 2, actual 4): rejected. The
	// rejection is a continuable error carrying the first/last segment positions.
	record, err = transform.Read()
	assert.Nil(t, record)
	assert.Error(t, err)
	assert.True(t, errs.IsErrTransformFailed(err))
	assert.Equal(t,
		"input '4_x12_210_txset_rejection.input.txt' transaction set rejected: "+
			"first segment no.8 (char[289,306]), last segment no.11 (char[375,390]): "+
			"segment 'SE' declared segment count 2, but actually 4",
		err.Error())

	// Transaction set 3 (ST*210*000158671, SE declares 3, actual 3): transforms fine.
	record, err = transform.Read()
	assert.NoError(t, err)
	assert.JSONEq(t,
		`{"invoice_number": "0000001603WW308", "amount_due": "1700"}`,
		string(record))

	record, err = transform.Read()
	assert.Nil(t, record)
	assert.Equal(t, io.EOF, err)
}

func Test3_NonValidatingReader(t *testing.T) {
	schemaFileReader, err := os.Open("./2_ups_edi_210.schema.json")
	assert.NoError(t, err)
	defer schemaFileReader.Close()

	inputFileReader, err := os.Open("./2_ups_edi_210.input.txt")
	assert.NoError(t, err)
	defer inputFileReader.Close()

	schemaContent, err := ioutil.ReadAll(schemaFileReader)
	assert.NoError(t, err)

	type ediSchema struct {
		FileDecl *edi.FileDecl `json:"file_declaration"`
	}
	var schema ediSchema
	err = json.Unmarshal(schemaContent, &schema)
	assert.NoError(t, err)

	type rawElem struct {
		ElemIndex int
		CompIndex int
		Data      string
	}
	type rawSeg struct {
		Name  string
		Raw   string
		Elems []rawElem
	}
	r := edi.NewNonValidatingReader(inputFileReader, schema.FileDecl)
	var rawSegs []rawSeg
	for {
		seg, err := r.Read()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
		rawSegs = append(rawSegs, rawSeg{
			Name: seg.Name,
			Raw:  string(seg.Raw),
			Elems: func() []rawElem {
				var elems []rawElem
				for _, e := range seg.Elems {
					elems = append(elems, rawElem{
						ElemIndex: e.ElemIndex,
						CompIndex: e.CompIndex,
						Data:      string(e.Data),
					})
				}
				return elems
			}(),
		})
	}
	cupaloy.SnapshotT(t, jsons.BPM(rawSegs))
}

// Benchmark1_CanadaPost_EDI_214-8   	    1627	    752143 ns/op	  398667 B/op	    9401 allocs/op
func Benchmark1_CanadaPost_EDI_214(b *testing.B) {
	tests[test1_CanadaPost_EDI_214].doBenchmark(b)
}

// Benchmark2_UPS_EDI_210-8          	     201	   6012683 ns/op	 3213400 B/op	   79062 allocs/op
func Benchmark2_UPS_EDI_210(b *testing.B) {
	tests[test2_UPS_EDI_210].doBenchmark(b)
}

// Benchmark3_X12_834-8              	    4438	    259565 ns/op	   77049 B/op	    2179 allocs/op
func Benchmark3_X12_834(b *testing.B) {
	tests[test3_X12_834].doBenchmark(b)
}
