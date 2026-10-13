package compiler

import (
	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/expand"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/types/coll"
)

func (c *Compiler) compileVectorFormLiteral(node *sexpr.Vector) error {
	return c.compileWithFormLiteralMetadata(node.Meta(), func() error {
		for _, elem := range node.Items {
			if err := c.CompileForm(elem); err != nil {
				return err
			}
		}
		c.emit(opcode.OpArray, len(node.Items))
		return nil
	})
}

func (c *Compiler) compileMapFormLiteral(node *sexpr.Map) error {
	return c.compileWithFormLiteralMetadata(node.Meta(), func() error {
		for _, entry := range node.Entries {
			if err := c.CompileForm(entry.Key); err != nil {
				return err
			}
			if err := c.CompileForm(entry.Value); err != nil {
				return err
			}
		}
		c.emit(opcode.OpHash, len(node.Entries)*2)
		return nil
	})
}

func (c *Compiler) compileSetFormLiteral(node *sexpr.Set) error {
	return c.compileWithFormLiteralMetadata(node.Meta(), func() error {
		for _, elem := range node.Items {
			if err := c.CompileForm(elem); err != nil {
				return err
			}
		}
		c.emit(opcode.OpSet, len(node.Items))
		return nil
	})
}

func (c *Compiler) compileWithFormLiteralMetadata(metadata *sexpr.Map, compileValue func() error) error {
	if metadata == nil || len(metadata.Entries) == 0 {
		return compileValue()
	}

	metadataPlan, err := expand.BuildMetadataLiteralPlan(metadata)
	if err != nil {
		return c.newCompileError("invalid literal metadata: %v", err)
	}
	runtimeMetadata, err := emit.MaterializeMetadata(metadataPlan)
	if err != nil {
		return c.newCompileError("invalid literal metadata: %v", err)
	}
	return c.compileWithLiteralMetadata(runtimeMetadata, compileValue)
}

func (c *Compiler) compileWithLiteralMetadata(metadata *coll.Map, compileValue func() error) error {
	if metadata == nil {
		return compileValue()
	}

	withMeta, ok := c.symbolTable.ResolveIntrinsic("with-meta")
	if !ok {
		return c.newCompileError("doggo.core/with-meta not found in symbol table")
	}

	c.loadSymbol(withMeta)
	if err := compileValue(); err != nil {
		return err
	}
	c.emit(opcode.OpConst, c.addConstant(metadata))
	c.emit(opcode.OpCall, 2)
	return nil
}
