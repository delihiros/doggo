package vm

import "testing"

func TestTransduce(t *testing.T) {
	runner := MustNewRunner()
	tests := []struct {
		input string
		want  string
	}{
		{`(transduce (map inc) + 0 [1 2 3])`, "9"},
		{`(transduce (filter even?) + 0 [1 2 3 4])`, "6"},
		{`(transduce (take 3) + 0 [1 2 3 4 5])`, "6"},
		{`(transduce (drop 2) + 0 [1 2 3 4 5])`, "12"},
		{`(transduce (comp (map inc) (filter even?) (take 2)) + 0 [1 2 3 4 5 6])`, "6"},
		{`(transduce (map inc) conj [] [1 2 3])`, "[2 3 4]"},
		{`(transduce (map inc) + 0 [])`, "0"},
	}

	for _, test := range tests {
		assertRunResult(t, runner, test.input, test.want)
	}
}

func TestIntoWithTransducer(t *testing.T) {
	runner := MustNewRunner()
	tests := []struct {
		input string
		want  string
	}{
		{`(into [] (map inc) [1 2 3])`, "[2 3 4]"},
		{`(into '() (map inc) [1 2 3])`, "(4 3 2)"},
		{`(into #{} (map inc) [1 1 2])`, "#{2 3}"},
		{`(into nil (map inc) [1 2])`, "(3 2)"},
		{`(do (defrecord User [name]) (:name (into (->User "Ada") (map identity) [[:name "Bob"]])))`, `"Bob"`},
	}

	for _, test := range tests {
		assertRunResult(t, runner, test.input, test.want)
	}
}

func TestTransducerErrorPropagation(t *testing.T) {
	runner := MustNewRunner()
	tests := []struct {
		input string
		want  string
	}{
		{`(transduce (map (fn [x] (/ 1 0))) conj [] [1])`, "division by zero"},
		{`(into [] (map (fn [x] (/ 1 0))) [1])`, "division by zero"},
		{`(into [] 1)`, "into: argument must be a collection, got number"},
		{`(into {} [1])`, "conj: argument must be a collection, got number"},
		{`(into 1 [2])`, "conj: unsupported collection type number"},
	}

	for _, test := range tests {
		assertRunErrorContains(t, runner, test.input, test.want)
	}
}
