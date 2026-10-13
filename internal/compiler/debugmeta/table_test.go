package debugmeta

import (
	"reflect"
	"testing"
)

func TestTableCloneReturnsDetachedSlices(t *testing.T) {
	table := Table{
		Lines:   []int{2, 3},
		Columns: []int{4, 5},
	}
	clone := table.Clone()
	clone.Lines[0] = 99
	clone.Columns[0] = 88

	if got, want := table.Lines, []int{2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("table lines = %#v, want %#v", got, want)
	}
	if got, want := table.Columns, []int{4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("table columns = %#v, want %#v", got, want)
	}
}
