package schema

import (
	"context"

	"doggo/internal/types"
)

func Explain(schema *Schema, value types.Value) Explanation {
	return ExplainWithContext(context.Background(), schema, value)
}

func ExplainWithContext(ctx context.Context, schema *Schema, value types.Value) Explanation {
	problems := validateProblems(ctx, schema, value, nil)
	return Explanation{
		Valid:    len(problems) == 0,
		Schema:   schema,
		Value:    value,
		Problems: problems,
	}
}
