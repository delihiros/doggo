package debugmeta

type Table struct {
	SourceFile string
	Lines      []int
	Columns    []int
}

func NewTable(sourceFile string) Table {
	return Table{
		SourceFile: sourceFile,
		Lines:      []int{},
		Columns:    []int{},
	}
}

func (t *Table) Record(line, column, count int) {
	if count <= 0 {
		return
	}
	for i := 0; i < count; i++ {
		t.Lines = append(t.Lines, line)
		t.Columns = append(t.Columns, column)
	}
}

func (t Table) Clone() Table {
	return Table{
		SourceFile: t.SourceFile,
		Lines:      append([]int(nil), t.Lines...),
		Columns:    append([]int(nil), t.Columns...),
	}
}
