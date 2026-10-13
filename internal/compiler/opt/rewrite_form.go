package opt

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

type FormPipelineInfo struct {
	Collection     sexpr.Form
	MapFn          sexpr.Form
	FilterPred     sexpr.Form
	ReduceFn       sexpr.Form
	ReduceInit     sexpr.Form
	HasReduceInit  bool
	IsVectorSource bool
	MapFirst       bool
}

func TryOptimizeFormList(list *sexpr.List, cfg *Config, resolver Resolver) (sexpr.Form, bool) {
	if cfg == nil || !cfg.EnableLoopFusion {
		return nil, false
	}
	if list == nil {
		return nil, false
	}
	if resolver == nil {
		return nil, false
	}

	if len(list.Items) == 4 && isReduceFormCall(list, resolver) {
		if shouldSkipFormFusion(list.Items[3], resolver) {
			return nil, false
		}
	}

	info := detectFormPipeline(list, resolver)
	if info == nil {
		return nil, false
	}

	var fused sexpr.Form
	if info.IsVectorSource {
		if canUseIndexBasedFusion(resolver) {
			fused = generateIndexBasedFusedFormLoop(info, list.Span())
		} else {
			return nil, false
		}
	} else {
		fused = generateFusedFormLoop(info, list.Span())
	}
	if fused == nil {
		return nil, false
	}
	return fused, true
}

func shouldSkipFormFusion(source sexpr.Form, resolver Resolver) bool {
	list, ok := source.(*sexpr.List)
	if !ok || len(list.Items) == 0 {
		return false
	}

	name, ok := formSymbolName(list.Items[0])
	if !ok {
		return false
	}

	switch name {
	case "range":
		binding, resolved := resolver.ResolveOptimizationSymbol(name)
		if !resolved {
			return false
		}
		return isGlobalLikeScope(binding.Scope) || binding.Scope == ScopeBuiltin
	case "map", "filter":
		binding, resolved := resolver.ResolveOptimizationSymbol(name)
		if !resolved {
			return false
		}
		if !isGlobalLikeScope(binding.Scope) && binding.Scope != ScopeBuiltin {
			return false
		}
		innermost := findInnermostFormSource(source, resolver)
		if isVectorFormSource(innermost) {
			return false
		}
		return true
	default:
		return false
	}
}

func findInnermostFormSource(source sexpr.Form, resolver Resolver) sexpr.Form {
	list, ok := source.(*sexpr.List)
	if !ok || len(list.Items) < 3 {
		return source
	}
	if isMapFormCall(list, resolver) || isFilterFormCall(list, resolver) {
		return findInnermostFormSource(list.Items[2], resolver)
	}
	return source
}

func isVectorFormSource(source sexpr.Form) bool {
	_, ok := source.(*sexpr.Vector)
	return ok
}

func detectFormPipeline(list *sexpr.List, resolver Resolver) *FormPipelineInfo {
	if !isReduceFormCall(list, resolver) {
		return nil
	}
	if len(list.Items) < 3 || len(list.Items) > 4 {
		return nil
	}

	info := &FormPipelineInfo{}
	if len(list.Items) == 4 {
		info.ReduceFn = list.Items[1]
		info.ReduceInit = list.Items[2]
		info.HasReduceInit = true
		return detectFormMapFilterChain(list.Items[3], info, resolver)
	}
	return nil
}

func detectFormMapFilterChain(expr sexpr.Form, info *FormPipelineInfo, resolver Resolver) *FormPipelineInfo {
	list, ok := expr.(*sexpr.List)
	if !ok {
		info.Collection = expr
		info.IsVectorSource = isVectorFormSource(expr)
		return info
	}
	if len(list.Items) < 2 {
		info.Collection = expr
		info.IsVectorSource = isVectorFormSource(expr)
		return info
	}

	if isMapFormCall(list, resolver) && len(list.Items) == 3 {
		if info.MapFn != nil {
			return nil
		}
		if info.FilterPred == nil {
			info.MapFirst = true
		}
		info.MapFn = list.Items[1]
		return detectFormMapFilterChain(list.Items[2], info, resolver)
	}

	if isFilterFormCall(list, resolver) && len(list.Items) == 3 {
		if info.FilterPred != nil {
			return nil
		}
		info.FilterPred = list.Items[1]
		return detectFormMapFilterChain(list.Items[2], info, resolver)
	}

	info.Collection = expr
	info.IsVectorSource = isVectorFormSource(expr)
	return info
}

func isReduceFormCall(list *sexpr.List, resolver Resolver) bool {
	if len(list.Items) < 1 {
		return false
	}
	name, ok := formSymbolName(list.Items[0])
	if !ok || name != "reduce" {
		return false
	}
	binding, resolved := resolver.ResolveOptimizationSymbol(name)
	if !resolved {
		return false
	}
	return isGlobalLikeScope(binding.Scope) || binding.Scope == ScopeBuiltin
}

func isMapFormCall(list *sexpr.List, resolver Resolver) bool {
	if len(list.Items) < 1 {
		return false
	}
	name, ok := formSymbolName(list.Items[0])
	if !ok || name != "map" {
		return false
	}
	binding, resolved := resolver.ResolveOptimizationSymbol(name)
	if !resolved {
		return false
	}
	return isGlobalLikeScope(binding.Scope) || binding.Scope == ScopeBuiltin
}

func isFilterFormCall(list *sexpr.List, resolver Resolver) bool {
	if len(list.Items) < 1 {
		return false
	}
	name, ok := formSymbolName(list.Items[0])
	if !ok || name != "filter" {
		return false
	}
	binding, resolved := resolver.ResolveOptimizationSymbol(name)
	if !resolved {
		return false
	}
	return isGlobalLikeScope(binding.Scope) || binding.Scope == ScopeBuiltin
}

func generateFusedFormLoop(info *FormPipelineInfo, sp span.Span) sexpr.Form {
	if info.MapFn == nil && info.FilterPred == nil {
		return nil
	}
	if info.MapFirst && info.FilterPred != nil {
		return nil
	}

	itemsSym := optSymbol("__items__", sp)
	accSym := optSymbol("__acc__", sp)
	itemSym := optSymbol("__item__", sp)

	bindings := optVector(sp,
		itemsSym, info.Collection,
		accSym, info.ReduceInit,
	)

	firstCall := optList(sp, optSymbol("first", sp), itemsSym)
	var bodyInner sexpr.Form

	if info.MapFn != nil && info.FilterPred != nil {
		mappedSym := optSymbol("__mapped__", sp)
		mapCall := optList(sp, info.MapFn, itemSym)
		filterCall := optList(sp, info.FilterPred, mappedSym)
		reduceCall := optList(sp, info.ReduceFn, accSym, mappedSym)
		recurWithReduce := optList(sp, optSymbol("recur", sp), optList(sp, optSymbol("rest", sp), itemsSym), reduceCall)
		recurSkip := optList(sp, optSymbol("recur", sp), optList(sp, optSymbol("rest", sp), itemsSym), accSym)
		ifFilter := optList(sp, optSymbol("if", sp), filterCall, recurWithReduce, recurSkip)
		letBinding := optVector(sp, mappedSym, mapCall)
		bodyInner = optList(sp, optSymbol("let", sp), letBinding, ifFilter)
	} else if info.MapFn != nil {
		mappedSym := optSymbol("__mapped__", sp)
		mapCall := optList(sp, info.MapFn, itemSym)
		reduceCall := optList(sp, info.ReduceFn, accSym, mappedSym)
		recurCall := optList(sp, optSymbol("recur", sp), optList(sp, optSymbol("rest", sp), itemsSym), reduceCall)
		letBinding := optVector(sp, mappedSym, mapCall)
		bodyInner = optList(sp, optSymbol("let", sp), letBinding, recurCall)
	} else if info.FilterPred != nil {
		filterCall := optList(sp, info.FilterPred, itemSym)
		reduceCall := optList(sp, info.ReduceFn, accSym, itemSym)
		recurWithReduce := optList(sp, optSymbol("recur", sp), optList(sp, optSymbol("rest", sp), itemsSym), reduceCall)
		recurSkip := optList(sp, optSymbol("recur", sp), optList(sp, optSymbol("rest", sp), itemsSym), accSym)
		bodyInner = optList(sp, optSymbol("if", sp), filterCall, recurWithReduce, recurSkip)
	}

	itemBinding := optVector(sp, itemSym, firstCall)
	letWithItem := optList(sp, optSymbol("let", sp), itemBinding, bodyInner)
	seqCall := optList(sp, optSymbol("seq", sp), itemsSym)
	ifBody := optList(sp, optSymbol("if", sp), seqCall, letWithItem, accSym)
	return optList(sp, optSymbol("loop", sp), bindings, ifBody)
}

func generateIndexBasedFusedFormLoop(info *FormPipelineInfo, sp span.Span) sexpr.Form {
	if info.MapFn == nil && info.FilterPred == nil {
		return nil
	}
	if info.MapFirst && info.FilterPred != nil {
		return nil
	}

	collSym := optSymbol("__coll__", sp)
	iSym := optSymbol("__i__", sp)
	lenSym := optSymbol("__len__", sp)
	accSym := optSymbol("__acc__", sp)
	itemSym := optSymbol("__item__", sp)

	countCall := optList(sp, optSymbol("count", sp), collSym)
	bindings := optVector(sp,
		collSym, info.Collection,
		iSym, sexpr.NewNumber("0", sp, nil),
		lenSym, countCall,
		accSym, info.ReduceInit,
	)

	nthCall := optList(sp, optSymbol("nth", sp), collSym, iSym)
	incCall := optList(sp, optSymbol("inc", sp), iSym)
	var bodyInner sexpr.Form

	if info.MapFn != nil && info.FilterPred != nil {
		mappedSym := optSymbol("__mapped__", sp)
		mapCall := optList(sp, info.MapFn, itemSym)
		filterCall := optList(sp, info.FilterPred, mappedSym)
		reduceCall := optList(sp, info.ReduceFn, accSym, mappedSym)

		recurWithReduce := optList(sp, optSymbol("recur", sp), collSym, incCall, lenSym, reduceCall)
		recurSkip := optList(sp, optSymbol("recur", sp), collSym, incCall, lenSym, accSym)
		ifFilter := optList(sp, optSymbol("if", sp), filterCall, recurWithReduce, recurSkip)
		letBinding := optVector(sp, mappedSym, mapCall)
		bodyInner = optList(sp, optSymbol("let", sp), letBinding, ifFilter)
	} else if info.MapFn != nil {
		mappedSym := optSymbol("__mapped__", sp)
		mapCall := optList(sp, info.MapFn, itemSym)
		reduceCall := optList(sp, info.ReduceFn, accSym, mappedSym)
		recurCall := optList(sp, optSymbol("recur", sp), collSym, incCall, lenSym, reduceCall)
		letBinding := optVector(sp, mappedSym, mapCall)
		bodyInner = optList(sp, optSymbol("let", sp), letBinding, recurCall)
	} else if info.FilterPred != nil {
		filterCall := optList(sp, info.FilterPred, itemSym)
		reduceCall := optList(sp, info.ReduceFn, accSym, itemSym)
		recurWithReduce := optList(sp, optSymbol("recur", sp), collSym, incCall, lenSym, reduceCall)
		recurSkip := optList(sp, optSymbol("recur", sp), collSym, incCall, lenSym, accSym)
		bodyInner = optList(sp, optSymbol("if", sp), filterCall, recurWithReduce, recurSkip)
	}

	itemBinding := optVector(sp, itemSym, nthCall)
	letWithItem := optList(sp, optSymbol("let", sp), itemBinding, bodyInner)
	ltCall := optList(sp, optSymbol("<", sp), iSym, lenSym)
	ifBody := optList(sp, optSymbol("if", sp), ltCall, letWithItem, accSym)
	return optList(sp, optSymbol("loop", sp), bindings, ifBody)
}

func formSymbolName(form sexpr.Form) (string, bool) {
	switch current := form.(type) {
	case sexpr.Symbol:
		return current.Name, true
	case sexpr.ResolvedSymbol:
		return current.Name, true
	default:
		return "", false
	}
}

func optSymbol(name string, sp span.Span) sexpr.Symbol {
	return sexpr.NewSymbol(name, sp, nil)
}

func optList(sp span.Span, items ...sexpr.Form) *sexpr.List {
	return sexpr.NewList(items, sp, nil)
}

func optVector(sp span.Span, items ...sexpr.Form) *sexpr.Vector {
	return sexpr.NewVector(items, sp, nil)
}
