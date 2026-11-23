package edi

import (
	"testing"

	"github.com/jf-tech/go-corelib/strs"
	"github.com/jf-tech/go-corelib/testlib"
	"github.com/stretchr/testify/assert"
)

func TestValidateFileDecl_Empty(t *testing.T) {
	err := (&ediValidateCtx{}).validateFileDecl(&FileDecl{})
	assert.Error(t, err)
	assert.Equal(t, `missing segment/segment_group with 'is_target' = true`, err.Error())
}

func TestValidateFileDecl_MinGreaterThanMax(t *testing.T) {
	err := (&ediValidateCtx{}).validateFileDecl(&FileDecl{
		SegDecls: []*SegDecl{
			{Name: "A", Children: []*SegDecl{{Name: "B", Max: testlib.IntPtr(0)}}},
		},
	})
	assert.Error(t, err)
	assert.Equal(t, `segment 'A/B' has 'min' value 1 > 'max' value 0`, err.Error())
}

func TestValidateFileDecl_TwoIsTarget(t *testing.T) {
	err := (&ediValidateCtx{}).validateFileDecl(&FileDecl{
		SegDecls: []*SegDecl{
			{Name: "A", IsTarget: true},
			{Name: "B", Type: strs.StrPtr(segTypeGroup), IsTarget: true},
		},
	})
	assert.Error(t, err)
	assert.Equal(t, `a second segment/group ('B') with 'is_target' = true is not allowed`, err.Error())
}

func TestValidateFileDecl_SegGroupHasNoChildren(t *testing.T) {
	err := (&ediValidateCtx{}).validateFileDecl(&FileDecl{
		SegDecls: []*SegDecl{
			{Name: "A", Type: strs.StrPtr(segTypeGroup), IsTarget: true},
		},
	})
	assert.Error(t, err)
	assert.Equal(t, `segment_group 'A' must have at least one child segment/segment_group`, err.Error())
}

func TestValidateFileDecl_Success(t *testing.T) {
	elem1 := Elem{Name: "be1", Index: 1}
	elem2 := Elem{Name: "be2c1", Index: 2, CompIndex: testlib.IntPtr(1)}
	elem3 := Elem{Name: "be2c2", Index: 2, CompIndex: testlib.IntPtr(2)}
	fd := &FileDecl{
		SegDecls: []*SegDecl{
			{Name: "A", Children: []*SegDecl{
				{Name: "B", IsTarget: true, Elems: []Elem{elem3, elem1, elem2}},
			}},
		},
	}
	err := (&ediValidateCtx{}).validateFileDecl(fd)
	assert.NoError(t, err)
	assert.Equal(t, "A", fd.SegDecls[0].fqdn)
	assert.Nil(t, fd.SegDecls[0].Elems)
	assert.Equal(t, "A/B", fd.SegDecls[0].Children[0].fqdn)
	assert.Equal(t, []Elem{elem3, elem1, elem2}, fd.SegDecls[0].Children[0].Elems)
}

func txSetTestValidFileDecl() *FileDecl {
	return &FileDecl{
		TxSet: &TxSetDecl{
			BeginSeg:         "ST",
			EndSeg:           "SE",
			CountElemIndex:   1,
			ControlElemIndex: testlib.IntPtr(2),
		},
		SegDecls: []*SegDecl{
			{
				Name:     "tx",
				Type:     strs.StrPtr(segTypeGroup),
				IsTarget: true,
				Max:      testlib.IntPtr(-1),
				Children: []*SegDecl{
					{Name: "ST"},
					{Name: "D", Min: testlib.IntPtr(0), Max: testlib.IntPtr(-1)},
					{Name: "SE"},
				},
			},
		},
	}
}

func TestValidateFileDecl_TxSet(t *testing.T) {
	for _, test := range []struct {
		name     string
		mutate   func(fd *FileDecl)
		expected string
	}{
		{
			name:     "success",
			mutate:   func(fd *FileDecl) {},
			expected: "",
		},
		{
			name: "success - count/control element index not specified",
			mutate: func(fd *FileDecl) {
				fd.TxSet.CountElemIndex = 0
				fd.TxSet.ControlElemIndex = nil
			},
			expected: "",
		},
		{
			name: "begin_segment missing",
			mutate: func(fd *FileDecl) {
				fd.TxSet.BeginSeg = ""
			},
			expected: "transaction_set: 'begin_segment' is required",
		},
		{
			name: "end_segment missing",
			mutate: func(fd *FileDecl) {
				fd.TxSet.EndSeg = ""
			},
			expected: "transaction_set: 'end_segment' is required",
		},
		{
			name: "begin_segment == end_segment",
			mutate: func(fd *FileDecl) {
				fd.TxSet.EndSeg = "ST"
			},
			expected: "transaction_set: 'begin_segment' and 'end_segment' must be different, both are 'ST'",
		},
		{
			name: "negative count_element_index",
			mutate: func(fd *FileDecl) {
				fd.TxSet.CountElemIndex = -1
			},
			expected: "transaction_set: 'count_element_index' must be >= 1, got -1",
		},
		{
			name: "zero control_element_index",
			mutate: func(fd *FileDecl) {
				fd.TxSet.ControlElemIndex = testlib.IntPtr(0)
			},
			expected: "transaction_set: 'control_element_index' must be >= 1, got 0",
		},
		{
			name: "begin segment not declared",
			mutate: func(fd *FileDecl) {
				fd.TxSet.BeginSeg = "XX"
			},
			expected: "transaction_set: begin segment 'XX' is not declared as a segment under the target segment/group",
		},
		{
			name: "begin segment is a group",
			mutate: func(fd *FileDecl) {
				fd.TxSet.BeginSeg = "tx"
			},
			expected: "transaction_set: begin segment 'tx' is not declared as a segment under the target segment/group",
		},
		{
			name: "end segment not declared",
			mutate: func(fd *FileDecl) {
				fd.TxSet.EndSeg = "XX"
			},
			expected: "transaction_set: end segment 'XX' is not declared as a segment under the target segment/group",
		},
		{
			name: "end segment has children",
			mutate: func(fd *FileDecl) {
				fd.SegDecls[0].Children[2].Children = []*SegDecl{{Name: "X"}}
			},
			expected: "transaction_set: end segment 'tx/SE' must not have child segments",
		},
		{
			name: "end segment min 0",
			mutate: func(fd *FileDecl) {
				fd.SegDecls[0].Children[2].Min = testlib.IntPtr(0)
			},
			expected: "transaction_set: end segment 'tx/SE' must have 'min' >= 1",
		},
		{
			name: "end segment not the last child",
			mutate: func(fd *FileDecl) {
				fd.SegDecls[0].Children = append(fd.SegDecls[0].Children, &SegDecl{Name: "Z", Min: testlib.IntPtr(0)})
			},
			expected: "transaction_set: segment 'tx/SE' on the path from end segment 'SE' to the target segment/group is not the last child of its parent 'tx'",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fd := txSetTestValidFileDecl()
			test.mutate(fd)
			err := (&ediValidateCtx{}).validateFileDecl(fd)
			if test.expected == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Equal(t, test.expected, err.Error())
			}
		})
	}
}
