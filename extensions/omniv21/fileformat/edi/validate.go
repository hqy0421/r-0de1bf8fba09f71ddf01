package edi

import (
	"errors"
	"fmt"

	"github.com/jf-tech/go-corelib/strs"
)

type ediValidateCtx struct {
	seenTarget bool
	targetDecl *SegDecl
}

func (ctx *ediValidateCtx) validateFileDecl(fileDecl *FileDecl) error {
	for _, segDecl := range fileDecl.SegDecls {
		if err := ctx.validateSegDecl(segDecl.Name, segDecl); err != nil {
			return err
		}
	}
	if !ctx.seenTarget {
		return errors.New("missing segment/segment_group with 'is_target' = true")
	}
	if fileDecl.TxSet != nil {
		if err := ctx.validateTxSetDecl(fileDecl.TxSet); err != nil {
			return err
		}
	}
	return nil
}

func (ctx *ediValidateCtx) validateSegDecl(segFQDN string, segDecl *SegDecl) error {
	segDecl.fqdn = segFQDN
	if segDecl.minOccurs() > segDecl.maxOccurs() {
		return fmt.Errorf(
			"segment '%s' has 'min' value %d > 'max' value %d", segFQDN, segDecl.minOccurs(), segDecl.maxOccurs())
	}
	if segDecl.IsTarget {
		if ctx.seenTarget {
			return fmt.Errorf("a second segment/group ('%s') with 'is_target' = true is not allowed", segFQDN)
		}
		ctx.seenTarget = true
		ctx.targetDecl = segDecl
	}
	if segDecl.isGroup() && len(segDecl.Children) <= 0 {
		return fmt.Errorf("segment_group '%s' must have at least one child segment/segment_group", segFQDN)
	}
	for _, child := range segDecl.Children {
		err := ctx.validateSegDecl(strs.BuildFQDN2(fqdnDelim, segFQDN, child.Name), child)
		if err != nil {
			return err
		}
	}
	return nil
}

// findSegDeclPath returns the path of segment decls from root (inclusive) down to the
// first (depth-first) non-group segment decl with the given name, or nil if not found.
func findSegDeclPath(root *SegDecl, name string) []*SegDecl {
	if !root.isGroup() && root.Name == name {
		return []*SegDecl{root}
	}
	for _, child := range root.Children {
		if path := findSegDeclPath(child, name); path != nil {
			return append([]*SegDecl{root}, path...)
		}
	}
	return nil
}

func (ctx *ediValidateCtx) validateTxSetDecl(txSet *TxSetDecl) error {
	if txSet.BeginSeg == "" {
		return errors.New("transaction_set: 'begin_segment' is required")
	}
	if txSet.EndSeg == "" {
		return errors.New("transaction_set: 'end_segment' is required")
	}
	if txSet.BeginSeg == txSet.EndSeg {
		return fmt.Errorf(
			"transaction_set: 'begin_segment' and 'end_segment' must be different, both are '%s'",
			txSet.BeginSeg)
	}
	if txSet.CountElemIndex < 0 {
		return fmt.Errorf(
			"transaction_set: 'count_element_index' must be >= 1, got %d", txSet.CountElemIndex)
	}
	if txSet.ControlElemIndex != nil && *txSet.ControlElemIndex < 1 {
		return fmt.Errorf(
			"transaction_set: 'control_element_index' must be >= 1, got %d", *txSet.ControlElemIndex)
	}
	if findSegDeclPath(ctx.targetDecl, txSet.BeginSeg) == nil {
		return fmt.Errorf(
			"transaction_set: begin segment '%s' is not declared as a segment under the target segment/group",
			txSet.BeginSeg)
	}
	endPath := findSegDeclPath(ctx.targetDecl, txSet.EndSeg)
	if endPath == nil {
		return fmt.Errorf(
			"transaction_set: end segment '%s' is not declared as a segment under the target segment/group",
			txSet.EndSeg)
	}
	endDecl := endPath[len(endPath)-1]
	if len(endDecl.Children) > 0 {
		return fmt.Errorf(
			"transaction_set: end segment '%s' must not have child segments", endDecl.fqdn)
	}
	if endDecl.minOccurs() < 1 {
		return fmt.Errorf(
			"transaction_set: end segment '%s' must have 'min' >= 1", endDecl.fqdn)
	}
	// The rejection of a transaction set is only precise if the end segment's
	// processing provably completes the target: every decl on the path from the end
	// segment up to (but excluding) the target must be the last child of its parent.
	for i := 1; i < len(endPath); i++ {
		parent, child := endPath[i-1], endPath[i]
		if parent.Children[len(parent.Children)-1] != child {
			return fmt.Errorf(
				"transaction_set: segment '%s' on the path from end segment '%s' to the target segment/group is not the last child of its parent '%s'",
				child.fqdn, txSet.EndSeg, parent.fqdn)
		}
	}
	return nil
}
