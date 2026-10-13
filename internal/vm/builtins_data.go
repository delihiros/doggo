package vm

import (
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type dataDiffParts struct {
	left         types.Value
	leftPresent  bool
	right        types.Value
	rightPresent bool
	both         types.Value
	bothPresent  bool
}

func dataDiffAbsent() dataDiffParts {
	return dataDiffParts{}
}

func dataDiffScalar(a, b types.Value) dataDiffParts {
	return dataDiffParts{
		left:         a,
		leftPresent:  true,
		right:        b,
		rightPresent: true,
	}
}

func dataDiffBoth(v types.Value) dataDiffParts {
	return dataDiffParts{
		both:        v,
		bothPresent: true,
	}
}

func dataDiffToValue(present bool, v types.Value) types.Value {
	if !present {
		return atom.NilValue
	}
	if v == nil {
		return atom.NilValue
	}
	return v
}

func (d dataDiffParts) Vector() types.Value {
	return coll.NewVector(
		dataDiffToValue(d.leftPresent, d.left),
		dataDiffToValue(d.rightPresent, d.right),
		dataDiffToValue(d.bothPresent, d.both),
	)
}

func valuesEqual(a, b types.Value) (bool, error) {
	if isNilArg(a) && isNilArg(b) {
		return true, nil
	}
	if isNilArg(a) || isNilArg(b) {
		return false, nil
	}
	return coll.ValueEqualsResult(a, b)
}

func isSequentialDiffValue(v types.Value) bool {
	if isNilArg(v) {
		return false
	}
	switch v.(type) {
	case atom.String, *coll.Map, *coll.Set:
		return false
	case *coll.Vector, *coll.List:
		return true
	case types.Seq, types.Sequable:
		return true
	default:
		return false
	}
}

func appendUniqueValues(dst []types.Value, values []types.Value) []types.Value {
	for _, value := range values {
		found := false
		for _, existing := range dst {
			if existing.Equals(value) {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, value)
		}
	}
	return dst
}

func sequableToSlice(seq types.Sequable) []types.Value {
	if seq == nil {
		return nil
	}
	items, _ := toSlice(seq.Seq())
	return items
}

func collapseMap(entries []types.Value) (types.Value, bool) {
	if len(entries) == 0 {
		return nil, false
	}
	return coll.NewMapFromPairs(entries), true
}

func collapseVector(items []types.Value, mask []bool) (types.Value, bool) {
	last := -1
	for i, present := range mask {
		if present {
			last = i
		}
	}
	if last < 0 {
		return nil, false
	}

	normalized := make([]types.Value, last+1)
	for i := 0; i <= last; i++ {
		if !mask[i] || items[i] == nil {
			normalized[i] = atom.NilValue
		} else {
			normalized[i] = items[i]
		}
	}
	return coll.NewVector(normalized...), true
}

func collapseSet(items []types.Value) (types.Value, bool) {
	if len(items) == 0 {
		return nil, false
	}
	return coll.NewSet(items...), true
}

func dataDiffValue(a, b types.Value) (dataDiffParts, error) {
	equal, err := valuesEqual(a, b)
	if err != nil {
		return dataDiffAbsent(), err
	}
	if equal {
		if isNilArg(a) {
			return dataDiffAbsent(), nil
		}
		return dataDiffBoth(a), nil
	}

	aMap, aIsMap := a.(types.Map)
	bMap, bIsMap := b.(types.Map)
	if aIsMap && bIsMap {
		return dataDiffMap(aMap, bMap)
	}

	aSet, aIsSet := a.(*coll.Set)
	bSet, bIsSet := b.(*coll.Set)
	if aIsSet && bIsSet {
		return dataDiffSet(aSet, bSet), nil
	}

	if isSequentialDiffValue(a) && isSequentialDiffValue(b) {
		return dataDiffSequential(a, b)
	}

	return dataDiffScalar(a, b), nil
}

func dataDiffMap(a, b types.Map) (dataDiffParts, error) {
	keys := appendUniqueValues(nil, sequableToSlice(a.Keys()))
	keys = appendUniqueValues(keys, sequableToSlice(b.Keys()))

	leftPairs := make([]types.Value, 0)
	rightPairs := make([]types.Value, 0)
	bothPairs := make([]types.Value, 0)

	for _, key := range keys {
		leftEntry := a.Find(key)
		rightEntry := b.Find(key)

		switch {
		case leftEntry != nil && rightEntry == nil:
			leftPairs = append(leftPairs, key, leftEntry.Value)
		case leftEntry == nil && rightEntry != nil:
			rightPairs = append(rightPairs, key, rightEntry.Value)
		case leftEntry != nil && rightEntry != nil:
			equal, err := valuesEqual(leftEntry.Value, rightEntry.Value)
			if err != nil {
				return dataDiffAbsent(), err
			}
			if equal {
				bothPairs = append(bothPairs, key, leftEntry.Value)
				continue
			}

			diff, err := dataDiffValue(leftEntry.Value, rightEntry.Value)
			if err != nil {
				return dataDiffAbsent(), err
			}
			if diff.leftPresent {
				leftPairs = append(leftPairs, key, diff.left)
			}
			if diff.rightPresent {
				rightPairs = append(rightPairs, key, diff.right)
			}
			if diff.bothPresent {
				bothPairs = append(bothPairs, key, diff.both)
			}
		}
	}

	leftValue, leftPresent := collapseMap(leftPairs)
	rightValue, rightPresent := collapseMap(rightPairs)
	bothValue, bothPresent := collapseMap(bothPairs)

	return dataDiffParts{
		left:         leftValue,
		leftPresent:  leftPresent,
		right:        rightValue,
		rightPresent: rightPresent,
		both:         bothValue,
		bothPresent:  bothPresent,
	}, nil
}

func dataDiffSequential(a, b types.Value) (dataDiffParts, error) {
	leftItems, err := toSlice(a)
	if err != nil {
		return dataDiffAbsent(), err
	}
	rightItems, err := toSlice(b)
	if err != nil {
		return dataDiffAbsent(), err
	}
	maxLen := len(leftItems)
	if len(rightItems) > maxLen {
		maxLen = len(rightItems)
	}

	leftOut := make([]types.Value, maxLen)
	rightOut := make([]types.Value, maxLen)
	bothOut := make([]types.Value, maxLen)
	leftMask := make([]bool, maxLen)
	rightMask := make([]bool, maxLen)
	bothMask := make([]bool, maxLen)

	for i := 0; i < maxLen; i++ {
		leftExists := i < len(leftItems)
		rightExists := i < len(rightItems)

		switch {
		case leftExists && !rightExists:
			leftOut[i] = leftItems[i]
			leftMask[i] = true
		case !leftExists && rightExists:
			rightOut[i] = rightItems[i]
			rightMask[i] = true
		case leftExists && rightExists:
			leftValue := leftItems[i]
			rightValue := rightItems[i]
			equal, err := valuesEqual(leftValue, rightValue)
			if err != nil {
				return dataDiffAbsent(), err
			}
			if equal {
				bothOut[i] = leftValue
				bothMask[i] = true
				continue
			}

			diff, err := dataDiffValue(leftValue, rightValue)
			if err != nil {
				return dataDiffAbsent(), err
			}
			if diff.leftPresent {
				leftOut[i] = diff.left
				leftMask[i] = true
			}
			if diff.rightPresent {
				rightOut[i] = diff.right
				rightMask[i] = true
			}
			if diff.bothPresent {
				bothOut[i] = diff.both
				bothMask[i] = true
			}
		}
	}

	leftValue, leftOK := collapseVector(leftOut, leftMask)
	rightValue, rightOK := collapseVector(rightOut, rightMask)
	bothValue, bothOK := collapseVector(bothOut, bothMask)

	return dataDiffParts{
		left:         leftValue,
		leftPresent:  leftOK,
		right:        rightValue,
		rightPresent: rightOK,
		both:         bothValue,
		bothPresent:  bothOK,
	}, nil
}

func dataDiffSet(a, b *coll.Set) dataDiffParts {
	leftOnly := make([]types.Value, 0)
	rightOnly := make([]types.Value, 0)
	both := make([]types.Value, 0)

	for _, item := range a.AllElements() {
		if b.Contains(item) {
			both = append(both, item)
		} else {
			leftOnly = append(leftOnly, item)
		}
	}
	for _, item := range b.AllElements() {
		if !a.Contains(item) {
			rightOnly = append(rightOnly, item)
		}
	}

	leftValue, leftPresent := collapseSet(leftOnly)
	rightValue, rightPresent := collapseSet(rightOnly)
	bothValue, bothPresent := collapseSet(both)

	return dataDiffParts{
		left:         leftValue,
		leftPresent:  leftPresent,
		right:        rightValue,
		rightPresent: rightPresent,
		both:         bothValue,
		bothPresent:  bothPresent,
	}
}

var builtinsData = []BuiltinDefinition{
	{
		Name:       "data-diff*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("data-diff*", args, 2); err != nil {
				return err
			}
			diff, diffErr := dataDiffValue(args[0], args[1])
			if diffErr != nil {
				return newArgumentError("%v", diffErr)
			}
			return diff.Vector()
		}},
	},
}
