package vm

import (
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestEDNParseReadOptionsAcceptsCanonicalOptions(t *testing.T) {
	readerFn := testEDNOptionCallable()
	defaultFn := testEDNOptionCallable()
	options, err := ednParseReadOptions("doggo.edn/read-string", coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("readers"), coll.NewMapFromPairs([]types.Value{
			types.Symbol("app/user"), readerFn,
			atom.NewString("app/date"), readerFn,
		}),
		atom.NewKeyword("default"), defaultFn,
		atom.NewKeyword("eof"), atom.NewKeyword("done"),
		atom.NewKeyword("location?"), atom.True,
	}), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(options.readers) != 2 {
		t.Fatalf("expected 2 readers, got %d", len(options.readers))
	}
	if options.defaultFn != defaultFn {
		t.Fatalf("defaultFn was not retained")
	}
	if !options.hasEOF || !options.eofValue.Equals(atom.NewKeyword("done")) {
		t.Fatalf("expected :eof value to be retained, got hasEOF=%v value=%v", options.hasEOF, options.eofValue)
	}
	if !options.location {
		t.Fatalf("expected location option to be true")
	}
}

func TestEDNParseReadOptionsRejectsUnsupportedOptions(t *testing.T) {
	_, err := ednParseReadOptions("doggo.edn/read-string", coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("pretty?"), atom.True,
	}), true)
	if err == nil || !strings.Contains(err.Error(), "unsupported option :pretty?") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEDNParseReadOptionsRejectsEOFForMultiFormReads(t *testing.T) {
	_, err := ednParseReadOptions("doggo.edn/read-string-all", coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("eof"), atom.NewKeyword("done"),
	}), false)
	if err == nil || !strings.Contains(err.Error(), "unsupported option :eof") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEDNParseReadOptionsRejectsInvalidLocationFlag(t *testing.T) {
	for _, value := range []types.Value{atom.NewNumberInt(1), atom.NilValue} {
		_, err := ednParseReadOptions("doggo.edn/read-string", coll.NewMapFromPairs([]types.Value{
			atom.NewKeyword("location?"), value,
		}), true)
		if err == nil || !strings.Contains(err.Error(), "option :location? must be boolean") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestEDNParseReadOptionsRejectsInvalidReaders(t *testing.T) {
	tests := []struct {
		name string
		opts types.Value
		want string
	}{
		{
			name: "readers must be map",
			opts: coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("readers"), coll.NewVector(),
			}),
			want: "option :readers must be map",
		},
		{
			name: "reader keys must be strings or symbols",
			opts: coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("readers"), coll.NewMapFromPairs([]types.Value{
					atom.NewKeyword("app/user"), testEDNOptionCallable(),
				}),
			}),
			want: "keys must be symbols or strings",
		},
		{
			name: "reader values must be callable",
			opts: coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("readers"), coll.NewMapFromPairs([]types.Value{
					types.Symbol("app/user"), atom.NewNumberInt(1),
				}),
			}),
			want: "option :readers[app/user] must be function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ednParseReadOptions("doggo.edn/read-string", tt.opts, true)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestEDNParseReadOptionsRejectsInvalidDefault(t *testing.T) {
	_, err := ednParseReadOptions("doggo.edn/read-string", coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("default"), atom.NewNumberInt(1),
	}), true)
	if err == nil || !strings.Contains(err.Error(), "option :default must be function") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEDNParseWriteOptionsAcceptsCanonicalOptions(t *testing.T) {
	options, err := ednParseWriteOptions("doggo.edn/write-string", coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("pretty?"), atom.True,
		atom.NewKeyword("indent"), atom.NewNumberInt(4),
		atom.NewKeyword("sort-keys?"), atom.True,
		atom.NewKeyword("newline?"), atom.True,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !options.pretty || options.indent != 4 || !options.sortKeys || !options.newline {
		t.Fatalf("unexpected parsed options: %+v", options)
	}
}

func TestEDNParseWriteOptionsRejectsUnsupportedOptions(t *testing.T) {
	_, err := ednParseWriteOptions("doggo.edn/write-string", coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("readers"), coll.NewEmptyMap(),
	}))
	if err == nil || !strings.Contains(err.Error(), "unsupported option :readers") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEDNParseWriteOptionsRejectsInvalidIndent(t *testing.T) {
	tests := []struct {
		name  string
		value types.Value
		want  string
	}{
		{name: "float", value: atom.NewNumberFloat(1.5), want: "option :indent must be a non-negative integer"},
		{name: "negative", value: atom.NewNumberInt(-1), want: "option :indent must be a non-negative integer"},
		{name: "non-number", value: atom.NewString("2"), want: "option :indent must be a non-negative integer"},
		{name: "nil", value: atom.NilValue, want: "option :indent must be a non-negative integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ednParseWriteOptions("doggo.edn/write-string", coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("indent"), tt.value,
			}))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestEDNParseWriteOptionsRejectsInvalidBooleans(t *testing.T) {
	for _, option := range []string{"pretty?", "sort-keys?", "newline?"} {
		t.Run(option, func(t *testing.T) {
			for _, value := range []types.Value{atom.NewNumberInt(1), atom.NilValue} {
				_, err := ednParseWriteOptions("doggo.edn/write-string", coll.NewMapFromPairs([]types.Value{
					atom.NewKeyword(option), value,
				}))
				want := "option :" + option + " must be boolean"
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected %q, got %v", want, err)
				}
			}
		})
	}
}

func TestEDNSortableScalarKeyClassifier(t *testing.T) {
	uuid, err := atom.NewUUID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatal(err)
	}

	for _, value := range []types.Value{
		atom.NilValue,
		atom.True,
		atom.NewNumberInt(1),
		atom.NewString("s"),
		atom.NewKeyword("k"),
		atom.NewCharacter('x'),
		types.Symbol("sym"),
		atom.NewInstantFromEpochMillis(0),
		uuid,
	} {
		if !ednIsSortableScalarKey(value) {
			t.Fatalf("expected %s (%s) to be sortable scalar key", value, value.Type())
		}
	}

	for _, value := range []types.Value{
		coll.NewVector(atom.NewNumberInt(1)),
		coll.NewList(atom.NewNumberInt(1)),
		coll.NewEmptyMap(),
		coll.NewSet(atom.NewNumberInt(1)),
	} {
		if ednIsSortableScalarKey(value) {
			t.Fatalf("expected %s (%s) to be non-scalar key", value, value.Type())
		}
	}
}

func testEDNOptionCallable() *Builtin {
	return &Builtin{Fn: func(args ...types.Value) types.Value {
		if len(args) == 0 {
			return atom.NilValue
		}
		return args[0]
	}}
}
