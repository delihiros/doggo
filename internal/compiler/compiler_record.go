package compiler

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func (c *Compiler) compileDefrecordForm(list *sexpr.List) error {
	if len(list.Items) < 3 {
		return c.newCompileError("defrecord requires name and fields")
	}

	nameVal := list.Items[1]
	name, ok := formSymbolName(nameVal)
	if !ok {
		return c.newCompileError("defrecord name must be a symbol, got %T", nameVal)
	}

	fieldsVec, ok := list.Items[2].(*sexpr.Vector)
	if !ok {
		return c.newCompileError("defrecord fields must be a vector, got %T", list.Items[2])
	}

	fields := make([]types.Value, 0, len(fieldsVec.Items))
	for _, field := range fieldsVec.Items {
		fieldName, ok := formSymbolName(field)
		if !ok {
			return c.newCompileError("defrecord field must be a symbol, got %T", field)
		}
		fields = append(fields, atom.NewKeyword(fieldName))
	}

	defSym, err := c.symbolTable.DefineProtectedGlobal(name)
	if err != nil {
		return c.newCompileError("%s", err)
	}
	defMeta := c.buildDefMetadata(nameVal, name, list)
	recordDef := coll.NewRecordDef(name, fields)
	qualifiedName := QualifySymbol(c.symbolTable.CurrentNamespace(), name)
	recordDef.QualifiedName = qualifiedName
	recordDef.Metadata = defMeta
	recordDefIdx := c.addConstant(recordDef)
	c.emit(opcode.OpConst, recordDefIdx)
	c.emit(opcode.OpSetGlobal, defSym.Index)

	if err := c.compileRecordPositionalConstructor(name, defSym, list, len(fieldsVec.Items)); err != nil {
		return err
	}
	if err := c.compileRecordMapConstructor(name, defSym, list); err != nil {
		return err
	}
	if err := c.compileRecordProtocolImplsForm(qualifiedName, list.Items[3:]); err != nil {
		return err
	}

	c.emit(opcode.OpNil)
	return nil
}

func (c *Compiler) compileRecordProtocolImplsForm(typeName string, items []sexpr.Form) error {
	var currentProtocol sexpr.Form

	for _, item := range items {
		if _, ok := formSymbolName(item); ok {
			currentProtocol = item
			continue
		}

		methodList, ok := item.(*sexpr.List)
		if !ok || currentProtocol == nil {
			continue
		}
		if len(methodList.Items) < 2 {
			continue
		}

		methodName, ok := formSymbolName(methodList.Items[0])
		if !ok {
			continue
		}
		params := methodList.Items[1]
		body := methodList.Items[2:]

		extendType, ok := c.symbolTable.ResolveIntrinsic("extend-type*")
		if !ok {
			return c.newCompileError("builtin 'extend-type*' not found")
		}
		c.emit(opcode.OpGetBuiltin, extendType.Index)

		if err := c.CompileForm(currentProtocol); err != nil {
			return err
		}

		typeNameIdx := c.addConstant(types.Symbol(typeName))
		c.emit(opcode.OpConst, typeNameIdx)

		methodKeyword := sexpr.NewKeyword(methodName, methodList.Span(), nil)
		fnItems := make([]sexpr.Form, 0, len(body)+2)
		fnItems = append(fnItems, sexpr.NewSymbol("fn", methodList.Span(), nil))
		fnItems = append(fnItems, params)
		fnItems = append(fnItems, body...)
		fnList := sexpr.NewList(fnItems, methodList.Span(), nil)
		implMap := sexpr.NewMap([]sexpr.MapEntry{{Key: methodKeyword, Value: fnList}}, methodList.Span(), nil)
		if err := c.CompileForm(implMap); err != nil {
			return err
		}

		c.emit(opcode.OpCall, 3)
		c.emit(opcode.OpPop)
	}

	return nil
}

func (c *Compiler) compileRecordPositionalConstructor(name string, defSym Symbol, sourceForm any, numFields int) error {
	return c.compileGeneratedPositionalConstructor(name, defSym, sourceForm, numFields, "make-record")
}

func (c *Compiler) compileRecordMapConstructor(name string, defSym Symbol, sourceForm any) error {
	ctorName := "map->" + name
	ctorSym := c.symbolTable.DefineGlobal(ctorName)
	ctorMeta := c.buildDefMetadata(types.Symbol(ctorName), ctorName, sourceForm)
	ctorMetaIndex := c.addConstant(ctorMeta) + 1
	c.updateGlobalPrivacy(ctorName, ctorMeta)

	sp := spanForGeneratedForm(sourceForm)
	fnList := sexpr.NewList([]sexpr.Form{
		sexpr.NewSymbol("fn", sp, nil),
		sexpr.NewVector([]sexpr.Form{sexpr.NewSymbol("m", sp, nil)}, sp, nil),
		sexpr.NewList([]sexpr.Form{
			sexpr.NewSymbol("make-record-from-map", sp, nil),
			sexpr.NewSymbol(defSym.Name, sp, nil),
			sexpr.NewSymbol("m", sp, nil),
		}, sp, nil),
	}, sp, nil)
	if err := c.CompileForm(fnList); err != nil {
		return err
	}

	c.emit(opcode.OpDefVar, ctorSym.Index, ctorMetaIndex)
	return nil
}

func (c *Compiler) compileDeftypeForm(list *sexpr.List) error {
	if len(list.Items) < 3 {
		return c.newCompileError("deftype requires name and fields")
	}

	nameVal := list.Items[1]
	name, ok := formSymbolName(nameVal)
	if !ok {
		return c.newCompileError("deftype name must be a symbol, got %T", nameVal)
	}

	fieldsVec, ok := list.Items[2].(*sexpr.Vector)
	if !ok {
		return c.newCompileError("deftype fields must be a vector, got %T", list.Items[2])
	}

	fields := make([]types.Value, 0, len(fieldsVec.Items))
	for _, field := range fieldsVec.Items {
		fieldName, ok := formSymbolName(field)
		if !ok {
			return c.newCompileError("deftype field must be a symbol, got %T", field)
		}
		fields = append(fields, atom.NewKeyword(fieldName))
	}

	defSym, err := c.symbolTable.DefineProtectedGlobal(name)
	if err != nil {
		return c.newCompileError("%s", err)
	}
	defMeta := c.buildDefMetadata(nameVal, name, list)
	typeDef := coll.NewDeftypeDef(name, fields)
	qualifiedName := QualifySymbol(c.symbolTable.CurrentNamespace(), name)
	typeDef.QualifiedName = qualifiedName
	typeDef.Metadata = defMeta
	typeDefIdx := c.addConstant(typeDef)
	c.emit(opcode.OpConst, typeDefIdx)
	c.emit(opcode.OpSetGlobal, defSym.Index)

	if err := c.compileDeftypeConstructor(name, defSym, list, len(fieldsVec.Items)); err != nil {
		return err
	}
	if err := c.compileDeftypeProtocolImplsForm(qualifiedName, list.Items[3:], fieldsVec); err != nil {
		return err
	}

	c.emit(opcode.OpNil)
	return nil
}

func (c *Compiler) compileDeftypeConstructor(name string, defSym Symbol, sourceForm any, numFields int) error {
	return c.compileGeneratedPositionalConstructor(name, defSym, sourceForm, numFields, "make-type-instance")
}

func (c *Compiler) compileGeneratedPositionalConstructor(name string, defSym Symbol, sourceForm any, numFields int, constructorBuiltin string) error {
	ctorName := "->" + name
	ctorSym := c.symbolTable.DefineGlobal(ctorName)
	ctorMeta := c.buildDefMetadata(types.Symbol(ctorName), ctorName, sourceForm)
	ctorMetaIndex := c.addConstant(ctorMeta) + 1
	c.updateGlobalPrivacy(ctorName, ctorMeta)

	sp := spanForGeneratedForm(sourceForm)
	params := make([]sexpr.Form, numFields)
	bodyItems := make([]sexpr.Form, 0, numFields+2)
	bodyItems = append(bodyItems, sexpr.NewSymbol(constructorBuiltin, sp, nil))
	bodyItems = append(bodyItems, sexpr.NewSymbol(defSym.Name, sp, nil))
	for i := 0; i < numFields; i++ {
		param := sexpr.NewSymbol("__arg"+string(rune('0'+i)), sp, nil)
		params[i] = param
		bodyItems = append(bodyItems, param)
	}

	fnList := sexpr.NewList([]sexpr.Form{
		sexpr.NewSymbol("fn", sp, nil),
		sexpr.NewVector(params, sp, nil),
		sexpr.NewList(bodyItems, sp, nil),
	}, sp, nil)
	if err := c.CompileForm(fnList); err != nil {
		return err
	}

	c.emit(opcode.OpDefVar, ctorSym.Index, ctorMetaIndex)
	return nil
}

func (c *Compiler) compileDeftypeProtocolImplsForm(typeName string, items []sexpr.Form, fieldsVec *sexpr.Vector) error {
	var currentProtocol sexpr.Form

	fieldNames := make([]string, 0, len(fieldsVec.Items))
	for _, field := range fieldsVec.Items {
		if fieldName, ok := formSymbolName(field); ok {
			fieldNames = append(fieldNames, fieldName)
		}
	}

	for _, item := range items {
		if _, ok := formSymbolName(item); ok {
			currentProtocol = item
			continue
		}

		methodList, ok := item.(*sexpr.List)
		if !ok || currentProtocol == nil {
			continue
		}
		if len(methodList.Items) < 2 {
			continue
		}

		methodName, ok := formSymbolName(methodList.Items[0])
		if !ok {
			continue
		}
		params := methodList.Items[1]
		body := methodList.Items[2:]

		paramsVec, ok := params.(*sexpr.Vector)
		if !ok || len(paramsVec.Items) == 0 {
			continue
		}
		thisParam, ok := formSymbolName(paramsVec.Items[0])
		if !ok {
			thisParam = "this"
		}

		wrappedBody := c.wrapBodyWithFieldBindingsForm(thisParam, fieldNames, body, methodList.Span())

		extendType, ok := c.symbolTable.ResolveIntrinsic("extend-type*")
		if !ok {
			return c.newCompileError("builtin 'extend-type*' not found")
		}
		c.emit(opcode.OpGetBuiltin, extendType.Index)

		if err := c.CompileForm(currentProtocol); err != nil {
			return err
		}

		typeNameIdx := c.addConstant(types.Symbol(typeName))
		c.emit(opcode.OpConst, typeNameIdx)

		methodKeyword := sexpr.NewKeyword(methodName, methodList.Span(), nil)
		fnItems := []sexpr.Form{
			sexpr.NewSymbol("fn", methodList.Span(), nil),
			params,
			wrappedBody,
		}
		fnList := sexpr.NewList(fnItems, methodList.Span(), nil)
		implMap := sexpr.NewMap([]sexpr.MapEntry{{Key: methodKeyword, Value: fnList}}, methodList.Span(), nil)
		if err := c.CompileForm(implMap); err != nil {
			return err
		}

		c.emit(opcode.OpCall, 3)
		c.emit(opcode.OpPop)
	}

	return nil
}

func (c *Compiler) wrapBodyWithFieldBindingsForm(thisParam string, fieldNames []string, body []sexpr.Form, sp span.Span) sexpr.Form {
	if len(fieldNames) == 0 {
		doItems := make([]sexpr.Form, 0, len(body)+1)
		doItems = append(doItems, sexpr.NewSymbol("do", sp, nil))
		doItems = append(doItems, body...)
		return sexpr.NewList(doItems, sp, nil)
	}

	bindings := make([]sexpr.Form, 0, len(fieldNames)*2)
	for _, fieldName := range fieldNames {
		bindings = append(bindings, sexpr.NewSymbol(fieldName, sp, nil))
		getFieldCall := sexpr.NewList([]sexpr.Form{
			sexpr.NewSymbol("get-field", sp, nil),
			sexpr.NewSymbol(thisParam, sp, nil),
			sexpr.NewKeyword(fieldName, sp, nil),
		}, sp, nil)
		bindings = append(bindings, getFieldCall)
	}
	bindingsVec := sexpr.NewVector(bindings, sp, nil)

	letItems := make([]sexpr.Form, 0, len(body)+2)
	letItems = append(letItems, sexpr.NewSymbol("let", sp, nil))
	letItems = append(letItems, bindingsVec)
	letItems = append(letItems, body...)
	return sexpr.NewList(letItems, sp, nil)
}

func spanForGeneratedForm(sourceForm any) span.Span {
	if form, ok := sourceForm.(sexpr.Form); ok {
		return form.Span()
	}
	return span.Unknown("<generated>")
}
