package vm

import (
	doggoerr "doggo/internal/errors"
	"doggo/internal/schema"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func schemaCheckReportValue(report *schema.CheckReport) types.Value {
	if report == nil {
		return atom.NilValue
	}

	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("pass?"), atom.NewBoolean(report.Pass),
		atom.NewKeyword("iterations"), atom.NewNumberInt(int64(report.Iterations)),
		atom.NewKeyword("seed"), atom.NewNumberInt(report.Seed),
		atom.NewKeyword("failing-input"), nilToValue(report.FailingInput),
		atom.NewKeyword("shrunk-input"), nilToValue(report.ShrunkInput),
		atom.NewKeyword("shrink-steps"), schemaCheckShrinkStepsValue(report.ShrinkSteps),
		atom.NewKeyword("failure"), schemaCheckFailureValue(report.Failure),
		atom.NewKeyword("explain"), schemaCheckExplainValue(report.Explain),
	})
}

func schemaCheckShrinkStepsValue(steps []types.Value) types.Value {
	if len(steps) == 0 {
		return coll.NewVector()
	}
	return coll.NewVector(steps...)
}

func schemaCheckFailureValue(failure *schema.CheckFailure) types.Value {
	if failure == nil {
		return atom.NilValue
	}

	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("kind"), failure.Kind,
		atom.NewKeyword("message"), atom.NewString(failure.Message),
		atom.NewKeyword("value"), nilToValue(failure.Value),
		atom.NewKeyword("exception"), schemaCheckFailureExceptionValue(failure.Exception),
	})
}

func schemaCheckFailureExceptionValue(data *doggoerr.ExceptionData) types.Value {
	if data == nil {
		return atom.NilValue
	}
	return canonicalExceptionMapValue(data)
}

func schemaCheckExplainValue(explanation *schema.Explanation) types.Value {
	if explanation == nil {
		return atom.NilValue
	}
	return explainValue(*explanation)
}
