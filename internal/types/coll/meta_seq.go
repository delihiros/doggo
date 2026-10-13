package coll

import (
	"doggo/internal/types"
)

// MetaSeq attaches metadata to generic Seq values such as lazy-seq and
// transformed sequences without changing their runtime shape.
type MetaSeq struct {
	SeqValue types.Seq
	Metadata *Map
}

func NewMetaSeq(seq types.Seq, metadata *Map) *MetaSeq {
	if wrapped, ok := seq.(*MetaSeq); ok {
		return &MetaSeq{
			SeqValue: wrapped.SeqValue,
			Metadata: metadata,
		}
	}
	return &MetaSeq{
		SeqValue: seq,
		Metadata: metadata,
	}
}

func (m *MetaSeq) First() types.Value {
	if m == nil || m.SeqValue == nil {
		return nil
	}
	return m.SeqValue.First()
}

func (m *MetaSeq) Next() types.Seq {
	if m == nil || m.SeqValue == nil {
		return nil
	}
	return m.SeqValue.Next()
}

func (m *MetaSeq) More() types.Seq {
	if m == nil || m.SeqValue == nil {
		return NewList()
	}
	return m.SeqValue.More()
}

func (m *MetaSeq) Seq() types.Value {
	if m == nil || m.SeqValue == nil {
		return nil
	}
	if sequable, ok := m.SeqValue.(types.Sequable); ok {
		seqValue := sequable.Seq()
		if seqValue == nil {
			return nil
		}
		if _, ok := seqValue.(*LazySeqError); ok {
			return seqValue
		}
	} else if m.SeqValue.First() == nil {
		return nil
	}
	return m
}

func (m *MetaSeq) String() string {
	if m == nil || m.SeqValue == nil {
		return "()"
	}
	return m.SeqValue.String()
}

func (m *MetaSeq) Type() string {
	if m == nil || m.SeqValue == nil {
		return "seq"
	}
	return m.SeqValue.Type()
}

func (m *MetaSeq) Hash() uint32 {
	if m == nil {
		return 0
	}
	return SeqHash(m)
}

func (m *MetaSeq) Equals(other types.Value) bool {
	return SequentialEquals(m, other)
}

func (m *MetaSeq) IsSelfEvaluating() bool {
	return false
}

func (m *MetaSeq) Realized() bool {
	if m == nil || m.SeqValue == nil {
		return true
	}
	if lazy, ok := m.SeqValue.(types.LazySeq); ok {
		return lazy.Realized()
	}
	return true
}

func (m *MetaSeq) ReduceDirect(fn types.ReduceFn, init types.Value) (types.Value, bool, error) {
	if m == nil || m.SeqValue == nil {
		return init, true, nil
	}
	if reducible, ok := m.SeqValue.(types.ReducibleSeq); ok {
		return reducible.ReduceDirect(fn, init)
	}
	return nil, false, nil
}
