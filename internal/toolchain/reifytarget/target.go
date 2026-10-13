package reifytarget

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"doggo/internal/project"
)

type Candidate struct {
	Source   string
	Required bool
}

type Resolved struct {
	Target     project.ReifyTarget
	SourceType types.Type
	Interface  *types.Interface
}

func Resolve(pkg *types.Package, name string) (Resolved, error) {
	if pkg == nil {
		return Resolved{}, fmt.Errorf("package is nil")
	}
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return Resolved{}, fmt.Errorf("type %s.%s not found", pkg.Path(), name)
	}
	typeName, ok := obj.(*types.TypeName)
	if !ok {
		return Resolved{}, fmt.Errorf("%s.%s is not a type", pkg.Path(), name)
	}
	if !typeName.Exported() {
		return Resolved{}, fmt.Errorf("type %s.%s is not exported", pkg.Path(), name)
	}

	sourceType := typeName.Type()
	if hasUnboundTypeParameters(sourceType) {
		return Resolved{}, fmt.Errorf("generic interface %s.%s requires type arguments", pkg.Path(), name)
	}
	named, ok := types.Unalias(sourceType).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return Resolved{}, fmt.Errorf("type %s.%s does not resolve to a package-owned named type", pkg.Path(), name)
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		return Resolved{}, fmt.Errorf("type %s.%s is not an interface", pkg.Path(), name)
	}
	iface.Complete()
	if !iface.IsMethodSet() {
		return Resolved{}, fmt.Errorf("type %s.%s is a constraint interface", pkg.Path(), name)
	}
	runtime, err := CanonicalTypeIdentity(named)
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve runtime identity for %s.%s: %w", pkg.Path(), name, err)
	}

	return Resolved{
		Target: project.ReifyTarget{
			Source:  pkg.Path() + "." + typeName.Name(),
			Runtime: runtime,
		},
		SourceType: sourceType,
		Interface:  iface,
	}, nil
}

func hasUnboundTypeParameters(t types.Type) bool {
	switch t := t.(type) {
	case *types.Alias:
		return t.TypeParams().Len() > t.TypeArgs().Len()
	case *types.Named:
		return t.TypeParams().Len() > t.TypeArgs().Len()
	default:
		return false
	}
}

func CanonicalTypeIdentity(t types.Type) (string, error) {
	if t == nil {
		return "", fmt.Errorf("nil type")
	}
	return new(identityEncoder).typeIdentity(types.Unalias(t))
}

type identityEncoder struct {
	interfaces []*types.Interface
}

func (e *identityEncoder) typeIdentity(t types.Type) (string, error) {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		return types.Typ[t.Kind()].Name(), nil
	case *types.Named:
		if hasUnboundTypeParameters(t) {
			return "", fmt.Errorf("unbound type parameters in %s", t.Obj().Name())
		}
		name := t.Obj().Name()
		if t.Obj().Pkg() != nil {
			name = t.Obj().Pkg().Path() + "." + name
		}
		if t.TypeArgs().Len() == 0 {
			return name, nil
		}
		args := make([]string, t.TypeArgs().Len())
		for i := range args {
			identity, err := e.typeIdentity(t.TypeArgs().At(i))
			if err != nil {
				return "", err
			}
			args[i] = identity
		}
		return name + "[" + strings.Join(args, ",") + "]", nil
	case *types.Pointer:
		return e.unaryIdentity("pointer", t.Elem())
	case *types.Slice:
		return e.unaryIdentity("slice", t.Elem())
	case *types.Array:
		elem, err := e.typeIdentity(t.Elem())
		if err != nil {
			return "", err
		}
		return "array[" + strconv.FormatInt(t.Len(), 10) + "](" + elem + ")", nil
	case *types.Map:
		key, err := e.typeIdentity(t.Key())
		if err != nil {
			return "", err
		}
		elem, err := e.typeIdentity(t.Elem())
		if err != nil {
			return "", err
		}
		return "map(" + key + "," + elem + ")", nil
	case *types.Chan:
		elem, err := e.typeIdentity(t.Elem())
		if err != nil {
			return "", err
		}
		return "chan[" + strconv.Itoa(int(t.Dir())) + "](" + elem + ")", nil
	case *types.Tuple:
		items := make([]string, t.Len())
		for i := range items {
			identity, err := e.typeIdentity(t.At(i).Type())
			if err != nil {
				return "", err
			}
			items[i] = identity
		}
		return "tuple(" + strings.Join(items, ",") + ")", nil
	case *types.Signature:
		if t.TypeParams().Len() > 0 || t.RecvTypeParams().Len() > 0 {
			return "", fmt.Errorf("unbound type parameters in signature")
		}
		params, err := e.typeIdentity(t.Params())
		if err != nil {
			return "", err
		}
		results, err := e.typeIdentity(t.Results())
		if err != nil {
			return "", err
		}
		return "signature[variadic=" + strconv.FormatBool(t.Variadic()) + "](" + params + "," + results + ")", nil
	case *types.Struct:
		fields := make([]string, t.NumFields())
		for i := range fields {
			field := t.Field(i)
			fieldType, err := e.typeIdentity(field.Type())
			if err != nil {
				return "", err
			}
			fields[i] = "field(" + strconv.Quote(types.Id(field.Pkg(), field.Name())) + ",embedded=" +
				strconv.FormatBool(field.Embedded()) + ",tag=" + strconv.Quote(t.Tag(i)) + "," + fieldType + ")"
		}
		return "struct(" + strings.Join(fields, ",") + ")", nil
	case *types.Interface:
		return e.interfaceIdentity(t)
	case *types.Union:
		return e.termsIdentity(normalizeTerms(unionTerms(t)))
	case *types.TypeParam:
		return "", fmt.Errorf("unbound type parameter %s", t.Obj().Name())
	default:
		return "", fmt.Errorf("unsupported type %T", t)
	}
}

func (e *identityEncoder) unaryIdentity(kind string, elem types.Type) (string, error) {
	identity, err := e.typeIdentity(elem)
	if err != nil {
		return "", err
	}
	return kind + "(" + identity + ")", nil
}

func (e *identityEncoder) interfaceIdentity(iface *types.Interface) (string, error) {
	iface.Complete()
	for id, active := range e.interfaces {
		if types.Identical(iface, active) {
			return "interface-ref(" + strconv.Itoa(id) + ")", nil
		}
	}
	id := len(e.interfaces)
	e.interfaces = append(e.interfaces, iface)
	defer func() { e.interfaces = e.interfaces[:id] }()

	methods := make([]string, iface.NumMethods())
	for i := range methods {
		method := iface.Method(i)
		signature, err := e.typeIdentity(method.Type())
		if err != nil {
			return "", err
		}
		methods[i] = "method(" + strconv.Quote(method.Id()) + "," + signature + ")"
	}
	slices.Sort(methods)

	restrictions, err := e.interfaceTypeSetIdentity(iface)
	if err != nil {
		return "", err
	}
	return "interface(" + strconv.Itoa(id) + ")[comparable=" + strconv.FormatBool(iface.IsComparable()) + "](" +
		strings.Join(methods, ",") + ";" + restrictions + ")", nil
}

type identityTerm struct {
	tilde bool
	typ   types.Type
}

type identityTypeSet struct {
	all        bool
	comparable bool
	terms      []identityTerm
}

func (e *identityEncoder) interfaceTypeSetIdentity(iface *types.Interface) (string, error) {
	set := collectInterfaceTypeSet(iface)
	if set.all {
		if set.comparable {
			return "comparable", nil
		}
		return "all", nil
	}
	if len(set.terms) == 0 {
		return "empty", nil
	}
	return e.termsIdentity(normalizeTerms(set.terms))
}

func collectInterfaceTypeSet(iface *types.Interface) identityTypeSet {
	set := identityTypeSet{all: true}
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		embedded := types.Unalias(iface.EmbeddedType(i))
		set = intersectTypeSets(set, embeddedTypeSet(embedded))
	}
	return set
}

func embeddedTypeSet(embedded types.Type) identityTypeSet {
	if types.Identical(embedded, types.Universe.Lookup("comparable").Type()) {
		return identityTypeSet{all: true, comparable: true}
	}
	if union, ok := embedded.(*types.Union); ok {
		return identityTypeSet{terms: normalizeTerms(unionTerms(union))}
	}
	if named, ok := embedded.(*types.Named); ok {
		if nested, ok := named.Underlying().(*types.Interface); ok {
			return collectInterfaceTypeSet(nested)
		}
	}
	if nested, ok := embedded.(*types.Interface); ok {
		return collectInterfaceTypeSet(nested)
	}
	return identityTypeSet{terms: []identityTerm{{typ: embedded}}}
}

func unionTerms(union *types.Union) []identityTerm {
	terms := make([]identityTerm, union.Len())
	for i := range terms {
		term := union.Term(i)
		terms[i] = identityTerm{tilde: term.Tilde(), typ: types.Unalias(term.Type())}
	}
	return terms
}

func intersectTypeSets(a, b identityTypeSet) identityTypeSet {
	comparable := a.comparable || b.comparable
	switch {
	case a.all && b.all:
		return identityTypeSet{all: true, comparable: comparable}
	case a.all:
		return restrictComparable(identityTypeSet{terms: b.terms}, comparable)
	case b.all:
		return restrictComparable(identityTypeSet{terms: a.terms}, comparable)
	}

	var terms []identityTerm
	for _, x := range a.terms {
		for _, y := range b.terms {
			if term, ok := intersectTerms(x, y); ok {
				terms = append(terms, term)
			}
		}
	}
	return restrictComparable(identityTypeSet{terms: normalizeTerms(terms)}, comparable)
}

func restrictComparable(set identityTypeSet, comparable bool) identityTypeSet {
	if !comparable {
		return set
	}
	set.terms = slices.DeleteFunc(set.terms, func(term identityTerm) bool {
		return !termIsComparable(term)
	})
	return set
}

func termIsComparable(term identityTerm) bool {
	t := types.Unalias(term.typ)
	if term.tilde {
		t = t.Underlying()
	}
	return types.Comparable(t)
}

func intersectTerms(a, b identityTerm) (identityTerm, bool) {
	if termsDisjoint(a, b) {
		return identityTerm{}, false
	}
	if !a.tilde || b.tilde {
		return a, true
	}
	return b, true
}

func normalizeTerms(terms []identityTerm) []identityTerm {
	var normalized []identityTerm
	for _, term := range terms {
		covered := false
		for i := 0; i < len(normalized); {
			switch {
			case termSubset(term, normalized[i]):
				covered = true
				i = len(normalized)
			case termSubset(normalized[i], term):
				normalized = slices.Delete(normalized, i, i+1)
			default:
				i++
			}
		}
		if !covered {
			normalized = append(normalized, term)
		}
	}
	return normalized
}

func termSubset(a, b identityTerm) bool {
	return !termsDisjoint(a, b) && (!a.tilde || b.tilde)
}

func termsDisjoint(a, b identityTerm) bool {
	aType := types.Unalias(a.typ)
	bType := types.Unalias(b.typ)
	if b.tilde {
		aType = aType.Underlying()
	}
	if a.tilde {
		bType = bType.Underlying()
	}
	return !types.Identical(aType, bType)
}

func (e *identityEncoder) termsIdentity(terms []identityTerm) (string, error) {
	identities := make([]string, len(terms))
	for i, term := range terms {
		identity, err := e.typeIdentity(term.typ)
		if err != nil {
			return "", err
		}
		if term.tilde {
			identity = "~" + identity
		}
		identities[i] = identity
	}
	slices.Sort(identities)
	return "union(" + strings.Join(identities, "|") + ")", nil
}
