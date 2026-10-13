package sexpr

import (
	"fmt"

	syntaxform "doggo/internal/syntax/form"
)

func LowerFile(nodes []*syntaxform.Node) (*File, error) {
	forms := make([]Form, 0, len(nodes))
	path := ""
	for _, node := range nodes {
		form, err := lowerNode(node)
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
		if path == "" && node != nil {
			path = node.Span.File
		}
	}
	return &File{
		Path:      path,
		Namespace: namespaceFromForms(forms),
		Forms:     forms,
	}, nil
}

func lowerNode(node *syntaxform.Node) (Form, error) {
	if node == nil {
		return Nil{}, nil
	}

	meta, err := lowerMetadata(node.Metadata)
	if err != nil {
		return nil, err
	}
	nodeSpan := node.Span
	nodeSpan.StartOffset = node.StartOffset
	nodeSpan.EndOffset = node.EndOffset
	if node.Metadata != nil {
		nodeSpan.Start = node.Metadata.Span.Start
		if nodeSpan.Start.Column > 1 {
			nodeSpan.Start.Column--
		}
	}
	base := baseForm{meta: meta, sp: nodeSpan}

	switch node.Kind {
	case syntaxform.KindSymbol:
		return Symbol{baseForm: base, Name: node.Text}, nil
	case syntaxform.KindNumber:
		return Number{baseForm: base, Literal: node.Number.Literal}, nil
	case syntaxform.KindString:
		return String{baseForm: base, Value: node.Text}, nil
	case syntaxform.KindKeyword:
		return Keyword{baseForm: base, Name: node.Text}, nil
	case syntaxform.KindRegex:
		return Regex{baseForm: base, Pattern: node.Text}, nil
	case syntaxform.KindChar:
		return Character{baseForm: base, Literal: node.Character.Literal}, nil
	case syntaxform.KindBoolean:
		return Bool{baseForm: base, Value: node.Boolean}, nil
	case syntaxform.KindNil:
		return Nil{baseForm: base}, nil
	case syntaxform.KindList:
		items, err := lowerItems(node.Items)
		if err != nil {
			return nil, err
		}
		return &List{baseForm: base, Items: items}, nil
	case syntaxform.KindVector:
		items, err := lowerItems(node.Items)
		if err != nil {
			return nil, err
		}
		return &Vector{baseForm: base, Items: items}, nil
	case syntaxform.KindSet:
		items, err := lowerItems(node.Items)
		if err != nil {
			return nil, err
		}
		return &Set{baseForm: base, Items: items}, nil
	case syntaxform.KindMap:
		entries := make([]MapEntry, 0, len(node.Entries))
		for _, entry := range node.Entries {
			key, err := lowerNode(entry.Key)
			if err != nil {
				return nil, err
			}
			value, err := lowerNode(entry.Value)
			if err != nil {
				return nil, err
			}
			entries = append(entries, MapEntry{Key: key, Value: value})
		}
		return &Map{baseForm: base, Entries: entries}, nil
	default:
		return nil, fmt.Errorf("unsupported syntax node kind %q", node.Kind)
	}
}

func lowerItems(nodes []*syntaxform.Node) ([]Form, error) {
	items := make([]Form, 0, len(nodes))
	for _, node := range nodes {
		form, err := lowerNode(node)
		if err != nil {
			return nil, err
		}
		items = append(items, form)
	}
	return items, nil
}

func lowerMetadata(node *syntaxform.Node) (*Map, error) {
	if node == nil {
		return nil, nil
	}
	form, err := lowerNode(node)
	if err != nil {
		return nil, err
	}
	meta, ok := form.(*Map)
	if !ok {
		return nil, fmt.Errorf("metadata must lower to map, got %T", form)
	}
	return meta, nil
}

func namespaceFromForms(forms []Form) string {
	for _, form := range forms {
		list, ok := form.(*List)
		if !ok || len(list.Items) < 2 {
			continue
		}
		head, ok := list.Items[0].(Symbol)
		if !ok || head.Name != "ns" {
			continue
		}
		ns, ok := list.Items[1].(Symbol)
		if !ok {
			continue
		}
		return ns.Name
	}
	return ""
}
