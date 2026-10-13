package gohelpers

import (
	"reflect"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// テスト用の構造体
type Person struct {
	Name string
	Age  int
}

type positionalPrivateFields struct {
	Visible string
	hidden  string
}

func TestInstantiateGoType_Struct(t *testing.T) {
	personType := reflect.TypeOf(Person{})

	// 引数なし
	instance, err := InstantiateGoType(personType, []types.Value{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	person, ok := instance.(*Person)
	if !ok {
		t.Fatalf("expected *Person, got %T", instance)
	}
	if person.Name != "" || person.Age != 0 {
		t.Errorf("expected zero values, got Name=%q, Age=%d", person.Name, person.Age)
	}
}

func TestInstantiateGoType_StructWithPositionalArgs(t *testing.T) {
	personType := reflect.TypeOf(Person{})

	// 位置引数
	args := []types.Value{
		atom.String("Alice"),
		atom.NewNumberInt(30),
	}
	instance, err := InstantiateGoType(personType, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	person, ok := instance.(*Person)
	if !ok {
		t.Fatalf("expected *Person, got %T", instance)
	}
	if person.Name != "Alice" || person.Age != 30 {
		t.Errorf("expected Name='Alice', Age=30, got Name=%q, Age=%d", person.Name, person.Age)
	}
}

func TestInstantiateGoType_StructWithKeywordArgs(t *testing.T) {
	personType := reflect.TypeOf(Person{})

	// キーワード引数
	args := []types.Value{
		atom.NewKeyword("Name"),
		atom.String("Bob"),
		atom.NewKeyword("Age"),
		atom.NewNumberInt(25),
	}
	instance, err := InstantiateGoType(personType, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	person, ok := instance.(*Person)
	if !ok {
		t.Fatalf("expected *Person, got %T", instance)
	}
	if person.Name != "Bob" || person.Age != 25 {
		t.Errorf("expected Name='Bob', Age=25, got Name=%q, Age=%d", person.Name, person.Age)
	}
}

func TestInstantiateGoType_StructWithMap(t *testing.T) {
	personType := reflect.TypeOf(Person{})

	// マップ引数
	mapArg := coll.NewMap(map[types.Value]types.Value{
		atom.NewKeyword("Name"): atom.String("Charlie"),
		atom.NewKeyword("Age"):  atom.NewNumberInt(35),
	})
	args := []types.Value{mapArg}
	instance, err := InstantiateGoType(personType, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	person, ok := instance.(*Person)
	if !ok {
		t.Fatalf("expected *Person, got %T", instance)
	}
	if person.Name != "Charlie" || person.Age != 35 {
		t.Errorf("expected Name='Charlie', Age=35, got Name=%q, Age=%d", person.Name, person.Age)
	}
}

func TestInstantiateGoType_PointerType(t *testing.T) {
	// ポインタ型
	ptrType := reflect.TypeOf((*int)(nil))

	instance, err := InstantiateGoType(ptrType, []types.Value{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ptr, ok := instance.(*int)
	if !ok {
		t.Fatalf("expected *int, got %T", instance)
	}
	if *ptr != 0 {
		t.Errorf("expected 0, got %d", *ptr)
	}
}

func TestInstantiateGoType_ValueTypeWithArg(t *testing.T) {
	instance, err := InstantiateGoType(reflect.TypeOf(""), []types.Value{atom.String("doggo")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := instance.(string); !ok || got != "doggo" {
		t.Fatalf("InstantiateGoType() = %#v, want doggo", instance)
	}
}

func TestSetStructFields_InvalidType(t *testing.T) {
	intVal := reflect.ValueOf(42)
	err := SetStructFields(intVal, []types.Value{})
	if err == nil {
		t.Error("expected error for non-struct value")
	}
}

func TestSetStructFields_WrongArgCount(t *testing.T) {
	person := Person{}
	rv := reflect.ValueOf(&person).Elem()

	// フィールド数と一致しない引数
	args := []types.Value{atom.String("only one")}
	err := SetStructFields(rv, args)
	if err == nil {
		t.Error("expected error for wrong argument count")
	}
}

func TestSetStructFieldsRejectsNilGoObjectCopy(t *testing.T) {
	person := Person{}
	err := SetStructFields(reflect.ValueOf(&person).Elem(), []types.Value{atom.NewGoObject(nil)})
	if err == nil {
		t.Fatal("SetStructFields() accepted a nil Go object copy")
	}
}

func TestSetStructFieldsRejectsUnexportedStructField(t *testing.T) {
	target := positionalPrivateFields{}
	err := SetStructFields(reflect.ValueOf(&target).Elem(), []types.Value{
		atom.String("visible"),
		atom.String("must-not-be-ignored"),
	})
	if err == nil {
		t.Fatal("SetStructFields() ignored an argument for an unexported field")
	}
}

func TestSetValueFromArgs_Empty(t *testing.T) {
	var i int
	rv := reflect.ValueOf(&i).Elem()

	err := SetValueFromArgs(rv, []types.Value{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if i != 0 {
		t.Errorf("expected 0, got %d", i)
	}
}

func TestSetValueFromArgs_SingleArg(t *testing.T) {
	var s string
	rv := reflect.ValueOf(&s).Elem()

	err := SetValueFromArgs(rv, []types.Value{atom.String("hello")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "hello" {
		t.Errorf("expected 'hello', got %q", s)
	}
}

func TestSetValueFromArgs_TooManyArgs(t *testing.T) {
	var i int
	rv := reflect.ValueOf(&i).Elem()

	err := SetValueFromArgs(rv, []types.Value{
		atom.NewNumberInt(1),
		atom.NewNumberInt(2),
	})
	if err == nil {
		t.Error("expected error for too many arguments")
	}
}
