package coll

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// seqPrintLimit は String() で表示する最大要素数です。
const seqPrintLimit = 10

// ponytail: unknown-length sequence value operations stop at this ceiling;
// add a context-aware budget if legitimate lazy map keys need to exceed it.
const unknownSeqValueOperationLimit = 10_000

// LazySeqAliasLimit bounds consecutive lazy aliases that produce no sequence
// node. Real collection elements use the larger value-operation limit above.
const LazySeqAliasLimit = 1_000

type lazySeqAliasState struct {
	depth int
}

var lazySeqAliasStates sync.Map

// LazySeqAliasTraversal shares the alias budget across nested Seq and
// RealizeSequenceNode calls on the current goroutine.
type LazySeqAliasTraversal struct {
	goroutineID uint64
	state       *lazySeqAliasState
	startDepth  int
	root        bool
}

func beginLazySeqAliasTraversal() (*LazySeqAliasTraversal, error) {
	goroutineID, err := util.CurrentGoroutineID()
	if err != nil {
		return nil, err
	}
	actual, loaded := lazySeqAliasStates.LoadOrStore(goroutineID, &lazySeqAliasState{})
	state := actual.(*lazySeqAliasState)
	traversal := &LazySeqAliasTraversal{
		goroutineID: goroutineID,
		state:       state,
		startDepth:  state.depth,
		root:        !loaded,
	}
	return traversal, nil
}

func BeginLazySeqAliasTraversal() (*LazySeqAliasTraversal, error) {
	traversal, err := beginLazySeqAliasTraversal()
	if err != nil {
		return nil, err
	}
	if !traversal.root {
		if err := traversal.Advance(); err != nil {
			return nil, err
		}
	}
	return traversal, nil
}

// BeginLazySeqAliasEdge starts a scope and charges one outgoing lazy-alias edge.
func BeginLazySeqAliasEdge() (*LazySeqAliasTraversal, error) {
	traversal, err := beginLazySeqAliasTraversal()
	if err != nil {
		return nil, err
	}
	if err := traversal.Advance(); err != nil {
		traversal.Close()
		return nil, err
	}
	return traversal, nil
}

func (traversal *LazySeqAliasTraversal) Advance() error {
	if traversal.state.depth >= LazySeqAliasLimit {
		return fmt.Errorf("sequence operation limit of %d lazy aliases exceeded; sequence may be infinite", LazySeqAliasLimit)
	}
	traversal.state.depth++
	return nil
}

func (traversal *LazySeqAliasTraversal) Close() {
	traversal.state.depth = traversal.startDepth
	if traversal.root {
		lazySeqAliasStates.Delete(traversal.goroutineID)
	}
}

type seqProvenance uint8

const (
	seqProvenanceNone seqProvenance = iota
	seqProvenancePotentiallyInfinite
	seqProvenanceDefinitelyInfinite
)

type seqProvenanceEntry struct {
	visiting   bool
	provenance seqProvenance
}

type seqProvenanceScan struct {
	memo                map[types.Seq]seqProvenanceEntry
	inspected           int
	structuralInspected int
	aliasDepth          int
}

func newSeqProvenanceScan() *seqProvenanceScan {
	return &seqProvenanceScan{memo: make(map[types.Seq]seqProvenanceEntry)}
}

// seqString は Seq の共通文字列化ロジックです。
func seqString(seq types.Seq) string {
	if seq == nil {
		return "()"
	}
	var b strings.Builder
	b.WriteByte('(')
	count := 0
	s := seq
	for s != nil && count < seqPrintLimit {
		if err := RealizeSequenceNode(s); err != nil {
			if count > 0 {
				b.WriteByte(' ')
			}
			b.WriteString((&LazySeqError{Err: err}).String())
			s = nil
			break
		}
		val := s.First()
		if val == nil {
			break
		}
		if count > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(val.String())
		count++
		s = s.Next()
	}
	// 残りがあれば ... を追加
	if s != nil {
		if err := RealizeSequenceNode(s); err != nil {
			b.WriteByte(' ')
			b.WriteString((&LazySeqError{Err: err}).String())
		} else if s.First() != nil {
			b.WriteString(" ...")
		}
	}
	b.WriteByte(')')
	return b.String()
}

// seqEquals は Seq の共通等価判定ロジックです。
func seqEquals(a, b types.Seq) bool {
	equal, _ := SeqEqualsResult(a, b)
	return equal
}

// SeqEqualsResult compares two sequences while preserving lazy evaluation
// errors that the Value.Equals signature cannot return directly.
func SeqEqualsResult(a, b types.Seq) (bool, error) {
	terminationKnown := hasKnownFiniteLength(a) || hasKnownFiniteLength(b)
	leftScan := newSeqProvenanceScan()
	rightScan := newSeqProvenanceScan()
	count := 0
	for {
		if decided, equal, err := sequenceProvenanceEqualityResult(a, b, leftScan, rightScan); decided || err != nil {
			return equal, err
		}
		refreshLeft := needsProvenanceRefresh(a)
		refreshRight := needsProvenanceRefresh(b)
		if err := RealizeSequenceNode(a); err != nil {
			return false, err
		}
		if err := RealizeSequenceNode(b); err != nil {
			return false, err
		}
		if refreshLeft || refreshRight {
			if refreshLeft {
				leftScan.forget(a)
			}
			if refreshRight {
				rightScan.forget(b)
			}
			if decided, equal, err := sequenceProvenanceEqualityResult(a, b, leftScan, rightScan); decided || err != nil {
				return equal, err
			}
		}
		if a == b {
			return true, nil
		}
		terminationKnown = terminationKnown || hasKnownFiniteLength(a) || hasKnownFiniteLength(b)
		var aFirst, bFirst types.Value
		if a != nil {
			aFirst = a.First()
		}
		if b != nil {
			bFirst = b.First()
		}
		if needsPostTraversalInspection(a) || needsPostTraversalInspection(b) {
			if needsPostTraversalInspection(a) {
				leftScan.forget(a)
			}
			if needsPostTraversalInspection(b) {
				rightScan.forget(b)
			}
			if decided, equal, err := sequenceProvenanceEqualityResult(a, b, leftScan, rightScan); decided || err != nil {
				return equal, err
			}
		}
		if aFirst == nil && bFirst == nil {
			return true, nil
		}
		if aFirst == nil || bFirst == nil {
			return false, nil
		}
		if !terminationKnown && count >= unknownSeqValueOperationLimit {
			return false, fmt.Errorf("sequence operation limit of %d elements exceeded while comparing; sequence may be infinite", unknownSeqValueOperationLimit)
		}
		equal, err := ValueEqualsResult(aFirst, bFirst)
		if err != nil {
			return false, err
		}
		if !equal {
			return false, nil
		}
		count++
		a = a.Next()
		b = b.Next()
	}
}

func sequenceProvenanceEqualityResult(a, b types.Seq, leftScan, rightScan *seqProvenanceScan) (bool, bool, error) {
	if err := realizedSeqError(a); err != nil {
		return false, false, err
	}
	if err := realizedSeqError(b); err != nil {
		return false, false, err
	}
	if types.Identical(a, b) {
		return true, true, nil
	}
	leftProvenance, err := leftScan.inspect(a)
	if err != nil {
		return false, false, err
	}
	rightProvenance, err := rightScan.inspect(b)
	if err != nil {
		return false, false, err
	}
	leftRange, leftIsRange := unwrapSeqForCoercion(a).(*InfiniteRangeLazySeq)
	rightRange, rightIsRange := unwrapSeqForCoercion(b).(*InfiniteRangeLazySeq)
	if leftIsRange && rightIsRange {
		return true, leftRange.Equals(rightRange), nil
	}
	if leftProvenance == seqProvenanceNone && rightProvenance == seqProvenanceNone {
		return false, false, nil
	}
	if leftProvenance == seqProvenanceDefinitelyInfinite && hasKnownFiniteLength(b) {
		return true, false, nil
	}
	if rightProvenance == seqProvenanceDefinitelyInfinite && hasKnownFiniteLength(a) {
		return true, false, nil
	}
	if (leftProvenance == seqProvenanceDefinitelyInfinite && rightProvenance == seqProvenanceNone) ||
		(rightProvenance == seqProvenanceDefinitelyInfinite && leftProvenance == seqProvenanceNone) {
		return false, false, nil
	}
	return true, false, fmt.Errorf("cannot compare distinct infinite sequences")
}

// SequentialEquals compares sequential values across concrete representations.
func SequentialEquals(a, b types.Value) bool {
	equal, _ := sequentialEqualsResult(a, b)
	return equal
}

func sequentialEqualsResult(a, b types.Value) (bool, error) {
	aSeq, aOK := SequentialSeq(a)
	bSeq, bOK := SequentialSeq(b)
	if !aOK || !bOK {
		return false, nil
	}
	return SeqEqualsResult(aSeq, bSeq)
}

// SequentialSeq normalizes sequential values to one shared sequence view.
func SequentialSeq(value types.Value) (types.Seq, bool) {
	switch value := value.(type) {
	case *Vector:
		if value == nil || len(value.Items) == 0 {
			return nil, true
		}
		return NewList(value.Items...), true
	case *types.MapEntry:
		if value == nil {
			return nil, false
		}
		return NewList(value.Key, value.Value), true
	case types.Seq:
		return value, true
	default:
		return nil, false
	}
}

// ValueEqualsResult preserves lazy sequence evaluation errors while comparing
// ordinary values with their existing equality implementation.
func ValueEqualsResult(a, b types.Value) (bool, error) {
	if a == nil || b == nil {
		return a == b, nil
	}
	leftSeq, leftOK := a.(types.Seq)
	rightSeq, rightOK := b.(types.Seq)
	if leftOK && rightOK {
		if err := realizedSeqError(leftSeq); err != nil {
			return false, err
		}
		if err := realizedSeqError(rightSeq); err != nil {
			return false, err
		}
		if types.Identical(a, b) {
			return true, nil
		}
		leftProvenance, err := newSeqProvenanceScan().inspect(leftSeq)
		if err != nil {
			return false, err
		}
		rightProvenance, err := newSeqProvenanceScan().inspect(rightSeq)
		if err != nil {
			return false, err
		}
		leftRange, leftIsRange := unwrapSeqForCoercion(leftSeq).(*InfiniteRangeLazySeq)
		rightRange, rightIsRange := unwrapSeqForCoercion(rightSeq).(*InfiniteRangeLazySeq)
		if leftIsRange && rightIsRange {
			return leftRange.Equals(rightRange), nil
		}
		if leftProvenance != seqProvenanceNone && rightProvenance != seqProvenanceNone {
			return false, fmt.Errorf("cannot compare distinct infinite sequences")
		}
	}
	if err := CollectionError(a); err != nil {
		return false, err
	}
	if err := CollectionError(b); err != nil {
		return false, err
	}
	aSeq, aIsSequential := SequentialSeq(a)
	bSeq, bIsSequential := SequentialSeq(b)
	if aIsSequential || bIsSequential {
		if !aIsSequential || !bIsSequential {
			return false, nil
		}
		return SeqEqualsResult(aSeq, bSeq)
	}
	if left, ok := a.(types.Map); ok {
		if _, ok := b.(types.Map); !ok {
			return false, nil
		}
		return mapValuesEqualResult(left, b)
	}

	switch left := a.(type) {
	case *Set:
		right, ok := b.(*Set)
		if !ok {
			return false, nil
		}
		return setValuesEqualResult(left, right)
	case *DeftypeInstance:
		right, ok := b.(*DeftypeInstance)
		if !ok || left.Def.identity() != right.Def.identity() {
			return false, nil
		}
		return valueSlicesEqualResult(left.Values, right.Values)
	case *types.MapEntry:
		right, ok := b.(*types.MapEntry)
		if !ok {
			return false, nil
		}
		keysEqual, err := ValueEqualsResult(left.Key, right.Key)
		if err != nil || !keysEqual {
			return keysEqual, err
		}
		return ValueEqualsResult(left.Value, right.Value)
	}
	return a.Equals(b), nil
}

func valueSlicesEqualResult(left, right []types.Value) (bool, error) {
	if len(left) != len(right) {
		return false, nil
	}
	for i := range left {
		equal, err := ValueEqualsResult(left[i], right[i])
		if err != nil || !equal {
			return equal, err
		}
	}
	return true, nil
}

func mapValuesEqualResult(left, right types.Value) (bool, error) {
	leftEntries, leftOK := mapEntriesForEquality(left)
	rightEntries, rightOK := mapEntriesForEquality(right)
	if !leftOK || !rightOK || len(leftEntries) != len(rightEntries) {
		return false, nil
	}
	if rightMap, ok := right.(*Map); ok {
		for _, leftEntry := range leftEntries {
			rightEntry, err := rightMap.lookupBucketResult(leftEntry.Key)
			if err != nil {
				return false, err
			}
			if rightEntry == nil {
				return false, nil
			}
			valuesEqual, err := ValueEqualsResult(leftEntry.Value, rightEntry.Value)
			if err != nil || !valuesEqual {
				return valuesEqual, err
			}
		}
		return true, nil
	}
	if _, ok := left.(*Map); ok {
		return mapValuesEqualResult(right, left)
	}

	// Non-hash map implementations do not expose a result-aware lookup.
	for _, leftEntry := range leftEntries {
		found := false
		for _, rightEntry := range rightEntries {
			keysEqual, err := ValueEqualsResult(leftEntry.Key, rightEntry.Key)
			if err != nil {
				return false, err
			}
			if !keysEqual {
				continue
			}
			valuesEqual, err := ValueEqualsResult(leftEntry.Value, rightEntry.Value)
			if err != nil || !valuesEqual {
				return valuesEqual, err
			}
			found = true
			break
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func mapEntriesForEquality(value types.Value) ([]*types.MapEntry, bool) {
	switch m := value.(type) {
	case *Map:
		entries := make([]*types.MapEntry, 0, m.CountVal)
		for _, bucket := range m.Buckets {
			entries = append(entries, bucket...)
		}
		return entries, true
	case *ArrayMap:
		entries := make([]*types.MapEntry, 0, len(m.Entries)/2)
		for i := 0; i+1 < len(m.Entries); i += 2 {
			entries = append(entries, types.NewMapEntry(m.Entries[i], m.Entries[i+1]))
		}
		return entries, true
	case *SortedMap:
		return m.Entries, true
	case *Record:
		entries := make([]*types.MapEntry, 0, m.Count())
		for i, field := range m.Def.Fields {
			entries = append(entries, types.NewMapEntry(field, m.Values[i]))
		}
		if m.Extras != nil {
			extraEntries, _ := mapEntriesForEquality(m.Extras)
			entries = append(entries, extraEntries...)
		}
		return entries, true
	case *StructMap:
		entries := make([]*types.MapEntry, 0, len(m.Values))
		for i, value := range m.Values {
			entries = append(entries, types.NewMapEntry(m.StructDef.keys[i], value))
		}
		return entries, true
	default:
		return nil, false
	}
}

func setValuesEqualResult(left, right *Set) (bool, error) {
	if left.CountVal != right.CountVal {
		return false, nil
	}
	for _, leftValue := range left.Order {
		found, _, err := right.lookupBucketResult(leftValue)
		if err != nil {
			return false, err
		}
		if found == nil {
			return false, nil
		}
	}
	return true, nil
}

// SeqHash は有限 Seq の要素ベースのハッシュを返します。
func SeqHash(seq types.Seq) uint32 {
	hash, _ := SeqHashResult(seq)
	return hash
}

// SeqHashResult hashes a finite sequence without discarding lazy errors.
func SeqHashResult(seq types.Seq) (uint32, error) {
	return seqHashResult(seq, newSeqProvenanceScan())
}

func seqHashResult(seq types.Seq, provenanceScan *seqProvenanceScan) (uint32, error) {
	hash := uint32(1)
	count := 0
	terminationKnown := hasKnownFiniteLength(seq)
	for seq != nil {
		provenance, err := provenanceScan.inspect(seq)
		if err != nil {
			return 0, err
		}
		if provenance != seqProvenanceNone {
			return 0, fmt.Errorf("cannot hash transformed infinite sequence")
		}
		terminationKnown = terminationKnown || hasKnownFiniteLength(seq)
		refreshProvenance := needsProvenanceRefresh(seq)
		if err := RealizeSequenceNode(seq); err != nil {
			return 0, err
		}
		if refreshProvenance {
			provenanceScan.forget(seq)
			provenance, err = provenanceScan.inspect(seq)
			if err != nil {
				return 0, err
			}
			if provenance != seqProvenanceNone {
				return 0, fmt.Errorf("cannot hash transformed infinite sequence")
			}
		}
		val := seq.First()
		if needsPostTraversalInspection(seq) {
			provenanceScan.forget(seq)
			provenance, err = provenanceScan.inspect(seq)
			if err != nil {
				return 0, err
			}
			if provenance != seqProvenanceNone {
				return 0, fmt.Errorf("cannot hash transformed infinite sequence")
			}
		}
		if val == nil {
			break
		}
		if !terminationKnown && count >= unknownSeqValueOperationLimit {
			return 0, fmt.Errorf("sequence operation limit of %d elements exceeded while hashing; sequence may be infinite", unknownSeqValueOperationLimit)
		}
		valueHash, err := ValueHashResult(val)
		if err != nil {
			return 0, err
		}
		hash = hash*31 + valueHash
		count++
		seq = seq.Next()
	}
	return util.MixCollectionHash(hash, count), nil
}

// ValueHashResult preserves lazy evaluation errors in nested hashable values.
func ValueHashResult(value types.Value) (uint32, error) {
	if value == nil {
		return 0, nil
	}
	if err := CollectionError(value); err != nil {
		return 0, err
	}
	if seq, ok := value.(types.Seq); ok {
		seq = unwrapSeqForCoercion(seq)
		provenanceScan := newSeqProvenanceScan()
		provenance, err := provenanceScan.inspect(seq)
		if err != nil {
			return 0, err
		}
		if infinite, ok := seq.(*InfiniteRangeLazySeq); ok {
			return infinite.Hash(), nil
		}
		if provenance != seqProvenanceNone {
			return 0, fmt.Errorf("cannot hash transformed infinite sequence")
		}
		return seqHashResult(seq, provenanceScan)
	}

	switch value := value.(type) {
	case *Vector:
		hash := uint32(1)
		for _, item := range value.Items {
			itemHash, err := ValueHashResult(item)
			if err != nil {
				return 0, err
			}
			hash = hash*31 + itemHash
		}
		return util.MixCollectionHash(hash, len(value.Items)), nil
	case *Map:
		return mapHashResult(mapEntriesForHashMap(value), value.CountVal)
	case *ArrayMap:
		entries, _ := mapEntriesForEquality(value)
		return mapHashResult(entries, len(value.Entries)/2)
	case *SortedMap:
		return mapHashResult(value.Entries, len(value.Entries))
	case *Record:
		entries, _ := mapEntriesForEquality(value)
		return mapHashResult(entries, value.Count())
	case *StructMap:
		var hash uint32
		for i, key := range value.StructDef.keys {
			if i >= len(value.Values) {
				break
			}
			keyHash, err := ValueHashResult(key)
			if err != nil {
				return 0, err
			}
			valueHash, err := ValueHashResult(value.Values[i])
			if err != nil {
				return 0, err
			}
			hash += keyHash*31 + valueHash
		}
		return util.MixCollectionHash(hash, len(value.Values)), nil
	case *Set:
		var hash uint32
		for _, item := range value.Order {
			itemHash, err := ValueHashResult(item)
			if err != nil {
				return 0, err
			}
			hash += itemHash
		}
		return util.MixCollectionHash(hash, value.CountVal), nil
	case *DeftypeInstance:
		hash := util.HashString(value.Def.identity())
		for i, item := range value.Values {
			if item == nil {
				continue
			}
			itemHash, err := ValueHashResult(item)
			if err != nil {
				return 0, err
			}
			hash = hash*31 + uint32(i) + itemHash
		}
		return hash, nil
	case *types.MapEntry:
		keyHash, err := ValueHashResult(value.Key)
		if err != nil {
			return 0, err
		}
		valueHash, err := ValueHashResult(value.Value)
		if err != nil {
			return 0, err
		}
		hash := uint32(1)
		hash = hash*31 + keyHash
		hash = hash*31 + valueHash
		return util.MixCollectionHash(hash, 2), nil
	default:
		return value.Hash(), nil
	}
}

func realizedSeqError(seq types.Seq) error {
	lazy, ok := seq.(types.LazySeq)
	if !ok || !lazy.Realized() {
		return nil
	}
	return RealizeSequenceNode(seq)
}

func (scan *seqProvenanceScan) inspect(seq types.Seq) (seqProvenance, error) {
	if seq == nil {
		return seqProvenanceNone, nil
	}
	seq = unwrapSeqForCoercion(seq)
	if err := realizedSeqError(seq); err != nil {
		return seqProvenanceNone, err
	}

	comparable := reflect.TypeOf(seq).Comparable()
	if comparable {
		if entry, ok := scan.memo[seq]; ok {
			if entry.visiting {
				return seqProvenanceNone, nil
			}
			return entry.provenance, nil
		}
	}

	provenance := seqProvenanceNone
	if _, ok := seq.(*InfiniteRangeLazySeq); ok {
		provenance = seqProvenanceDefinitelyInfinite
	}
	if marked, ok := seq.(interface{ KnownInfinite() bool }); ok && marked.KnownInfinite() {
		provenance = seqProvenanceDefinitelyInfinite
	}
	knownFinite := hasKnownFiniteLength(seq)
	_, structural := seq.(*consSeq)
	budgetExhausted := false
	if !knownFinite && provenance == seqProvenanceNone {
		budgetExhausted = scan.inspected >= unknownSeqValueOperationLimit
		if structural {
			budgetExhausted = scan.structuralInspected >= unknownSeqValueOperationLimit
		}
	}
	if budgetExhausted {
		if provenance == seqProvenanceNone {
			provenance = seqProvenancePotentiallyInfinite
		}
		if comparable {
			scan.memo[seq] = seqProvenanceEntry{provenance: provenance}
		}
		return provenance, nil
	}
	if !knownFinite && provenance == seqProvenanceNone {
		if structural {
			scan.structuralInspected++
		} else {
			scan.inspected++
		}
	}
	if comparable {
		scan.memo[seq] = seqProvenanceEntry{visiting: true}
	}

	preservesInfinite := false
	if preserving, ok := seq.(interface{ PreservesInfiniteLength() bool }); ok {
		preservesInfinite = preserving.PreservesInfiniteLength()
	}
	hasBoundedSourceWorkPerNode := knownFinite
	if bounded, ok := seq.(interface{ HasBoundedSourceWorkPerNode() bool }); ok {
		hasBoundedSourceWorkPerNode = hasBoundedSourceWorkPerNode || bounded.HasBoundedSourceWorkPerNode()
	}
	mergeSource := func(sourceProvenance seqProvenance) {
		if sourceProvenance == seqProvenanceNone {
			return
		}
		if hasBoundedSourceWorkPerNode && sourceProvenance == seqProvenanceDefinitelyInfinite {
			return
		}
		inherited := seqProvenancePotentiallyInfinite
		if preservesInfinite && sourceProvenance == seqProvenanceDefinitelyInfinite {
			inherited = seqProvenanceDefinitelyInfinite
		}
		if inherited > provenance {
			provenance = inherited
		}
	}
	hasDeclaredSource := false
	if sourced, ok := seq.(interface{ Source() types.Seq }); ok {
		source := sourced.Source()
		if source != nil {
			hasDeclaredSource = true
			sourceProvenance, err := scan.inspect(source)
			if err != nil {
				return seqProvenanceNone, err
			}
			mergeSource(sourceProvenance)
		}
	}
	if sourced, ok := seq.(interface{ Sources() []types.Seq }); ok {
		sources := sourced.Sources()
		hasDeclaredSource = hasDeclaredSource || len(sources) > 0
		for _, source := range sources {
			sourceProvenance, err := scan.inspect(source)
			if err != nil {
				return seqProvenanceNone, err
			}
			mergeSource(sourceProvenance)
		}
	}
	if provenance == seqProvenanceNone {
		if realizer, ok := seq.(interface{ RealizeNode() error }); ok {
			if err := realizer.RealizeNode(); err != nil {
				return seqProvenanceNone, err
			}
		}
		if realized, ok := seq.(interface{ RealizedSequence() types.Seq }); ok {
			inner := realized.RealizedSequence()
			_, innerIsLazy := inner.(types.LazySeq)
			if inner == seq {
				provenance = seqProvenancePotentiallyInfinite
			} else if inner != nil && (!hasDeclaredSource || innerIsLazy) {
				if innerIsLazy && scan.aliasDepth >= LazySeqAliasLimit {
					provenance = seqProvenancePotentiallyInfinite
				} else {
					if innerIsLazy {
						scan.aliasDepth++
					}
					innerProvenance, err := scan.inspect(inner)
					if innerIsLazy {
						scan.aliasDepth--
					}
					if err != nil {
						return seqProvenanceNone, err
					}
					if innerProvenance > provenance {
						provenance = innerProvenance
					}
				}
			}
		}
	}
	if comparable {
		scan.memo[seq] = seqProvenanceEntry{provenance: provenance}
	}
	return provenance, nil
}

func needsProvenanceRefresh(seq types.Seq) bool {
	lazy, ok := seq.(types.LazySeq)
	return ok && !lazy.Realized()
}

func needsPostTraversalInspection(seq types.Seq) bool {
	_, ok := seq.(interface{ RealizeNode() error })
	return ok
}

func (scan *seqProvenanceScan) forget(seq types.Seq) {
	if seq == nil {
		return
	}
	seq = unwrapSeqForCoercion(seq)
	if reflect.TypeOf(seq).Comparable() {
		delete(scan.memo, seq)
	}
}

func hasKnownFiniteLength(seq types.Seq) bool {
	if seq == nil {
		return true
	}
	if counted, ok := seq.(types.Countable); ok && counted.Count() >= 0 {
		return true
	}
	known, ok := seq.(interface{ KnownFinite() bool })
	return ok && known.KnownFinite()
}

// CollectionError reports deferred hash/equality errors stored while building
// hashed collections. Callers must not expose such collections as valid values.
func CollectionError(value types.Value) error {
	switch value := value.(type) {
	case *Map:
		if value != nil {
			return value.hashErr
		}
	case *Set:
		if value != nil {
			return value.hashErr
		}
	case *Record:
		if value != nil && value.Extras != nil {
			return value.Extras.hashErr
		}
	case *TransientMap:
		if value != nil {
			value.mu.RLock()
			defer value.mu.RUnlock()
			return value.hashErr
		}
	}
	return nil
}

func mapEntriesForHashMap(value *Map) []*types.MapEntry {
	entries := make([]*types.MapEntry, 0, value.CountVal)
	for _, bucket := range value.Buckets {
		entries = append(entries, bucket...)
	}
	return entries
}

func mapHashResult(entries []*types.MapEntry, count int) (uint32, error) {
	var hash uint32
	for _, entry := range entries {
		keyHash, err := ValueHashResult(entry.Key)
		if err != nil {
			return 0, err
		}
		valueHash, err := ValueHashResult(entry.Value)
		if err != nil {
			return 0, err
		}
		hash += keyHash*31 + valueHash
	}
	return util.MixCollectionHash(hash, count), nil
}

// RealizeSequenceNode realizes the current sequence node and returns a stored lazy
// evaluation error. Lazy sequence implementations expose errors through the
// existing Sequable.Seq sentinel contract.
func RealizeSequenceNode(seq types.Seq) error {
	return realizeSequenceNode(seq, nil, nil)
}

func realizeSequenceNode(seq types.Seq, visiting map[types.Seq]struct{}, traversal *LazySeqAliasTraversal) error {
	if seq == nil {
		return nil
	}
	seq = unwrapSeqForCoercion(seq)
	comparable := reflect.TypeOf(seq).Comparable()
	if visiting != nil && comparable {
		if _, ok := visiting[seq]; ok {
			return fmt.Errorf("recursive lazy sequence realization")
		}
		visiting[seq] = struct{}{}
		defer delete(visiting, seq)
	}
	if realizer, ok := seq.(interface{ RealizeNode() error }); ok {
		if err := realizer.RealizeNode(); err != nil {
			return err
		}
		if realized, ok := seq.(interface{ RealizedSequence() types.Seq }); ok {
			inner := realized.RealizedSequence()
			if inner == seq {
				return fmt.Errorf("recursive lazy sequence realization")
			}
			if inner != nil {
				if visiting == nil {
					visiting = make(map[types.Seq]struct{})
					if comparable {
						visiting[seq] = struct{}{}
					}
				}
				if _, lazy := inner.(types.LazySeq); lazy {
					ownedTraversal := false
					if traversal == nil {
						var err error
						traversal, err = BeginLazySeqAliasTraversal()
						if err != nil {
							return err
						}
						ownedTraversal = true
					}
					if err := traversal.Advance(); err != nil {
						if ownedTraversal {
							traversal.Close()
						}
						return err
					}
					if ownedTraversal {
						err := realizeSequenceNode(inner, visiting, traversal)
						traversal.Close()
						return err
					}
				}
				return realizeSequenceNode(inner, visiting, traversal)
			}
		}
		return nil
	}
	sequable, ok := seq.(types.Sequable)
	if !ok {
		return nil
	}
	ownedTraversal := false
	if traversal == nil && hasLazySeqSource(seq) {
		var err error
		traversal, err = BeginLazySeqAliasTraversal()
		if err != nil {
			return err
		}
		ownedTraversal = true
	}
	seqValue := sequable.Seq()
	if ownedTraversal {
		traversal.Close()
	}
	lazyErr, ok := seqValue.(*LazySeqError)
	if !ok {
		return nil
	}
	return lazyErr.Cause()
}

func hasLazySeqSource(seq types.Seq) bool {
	if _, lazy := seq.(types.LazySeq); !lazy {
		return false
	}
	if sourced, ok := seq.(interface{ Source() types.Seq }); ok {
		_, lazy := sourced.Source().(types.LazySeq)
		if lazy {
			return true
		}
	}
	if sourced, ok := seq.(interface{ Sources() []types.Seq }); ok {
		for _, source := range sourced.Sources() {
			if _, lazy := source.(types.LazySeq); lazy {
				return true
			}
		}
	}
	return false
}

// seqMore は Seq の共通 More() 実装です。
func seqMore(seq types.Seq) types.Seq {
	if next := seq.Next(); next != nil {
		return next
	}
	return NewList()
}

// SeqString は Seq の文字列表現を返します（エクスポート版）。
func SeqString(seq types.Seq) string {
	return seqString(seq)
}

// SeqBase は Seq 実装の共通部分を提供する埋め込み用構造体です。
type SeqBase struct{}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (SeqBase) IsSelfEvaluating() bool {
	return false
}
