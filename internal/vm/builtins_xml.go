package vm

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var (
	xmlTagKey     = atom.NewKeyword("tag")
	xmlAttrsKey   = atom.NewKeyword("attrs")
	xmlContentKey = atom.NewKeyword("content")
)

type xmlNode struct {
	tag     string
	attrs   []xmlAttr
	content []xmlContent
}

type xmlAttr struct {
	name  string
	value string
}

type xmlContent struct {
	text string
	node *xmlNode
}

var builtinsXML = []BuiltinDefinition{
	{
		Name:       "xml-parse-string*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("doggo.xml/read-string", args, 1); err != nil {
				return nil, err
			}

			input, ok := args[0].(atom.String)
			if !ok {
				return nil, xmlInvalidArg("doggo.xml/read-string", "expected string input, got %s", args[0].Type())
			}

			node, err := xmlParseDocument(string(input))
			if err != nil {
				return nil, xmlInvalidArg("doggo.xml/read-string", "invalid XML: %v", err)
			}
			return xmlNodeToDoggo(node), nil
		}},
	},
	{
		Name:       "xml-emit-string*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			_ = ctx
			if err := CheckArgCountCtx("doggo.xml/write-string", args, 1); err != nil {
				return nil, err
			}

			node, err := xmlDoggoToNode(args[0])
			if err != nil {
				return nil, err
			}

			var sb strings.Builder
			sb.WriteString("<?xml version='1.0' encoding='UTF-8'?>\n")
			if err := xmlWriteNode(&sb, node); err != nil {
				return nil, err
			}
			return atom.NewString(sb.String()), nil
		}},
	},
}

func xmlInvalidArg(label, format string, args ...any) error {
	return invalidArgumentVMError(label, fmt.Sprintf("%s: %s", label, fmt.Sprintf(format, args...)))
}

func xmlParseDocument(input string) (*xmlNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(input))
	root, err := xmlReadDocument(decoder)
	if err != nil {
		return nil, err
	}
	return root, nil
}

func xmlReadDocument(decoder *xml.Decoder) (*xmlNode, error) {
	for {
		token, err := decoder.RawToken()
		if err == io.EOF {
			return nil, fmt.Errorf("missing root element")
		}
		if err != nil {
			return nil, err
		}

		switch tok := token.(type) {
		case xml.StartElement:
			node, err := xmlReadElement(decoder, tok)
			if err != nil {
				return nil, err
			}
			if err := xmlEnsureNoTrailingContent(decoder); err != nil {
				return nil, err
			}
			return node, nil
		case xml.CharData:
			if strings.TrimSpace(string(tok)) == "" {
				continue
			}
			return nil, fmt.Errorf("unexpected text outside root element")
		case xml.Comment, xml.Directive, xml.ProcInst:
			continue
		default:
			return nil, fmt.Errorf("unexpected token %T", token)
		}
	}
}

func xmlEnsureNoTrailingContent(decoder *xml.Decoder) error {
	for {
		token, err := decoder.RawToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch tok := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(tok)) == "" {
				continue
			}
			return fmt.Errorf("unexpected trailing text")
		case xml.Comment, xml.Directive, xml.ProcInst:
			continue
		default:
			return fmt.Errorf("unexpected trailing XML content")
		}
	}
}

func xmlReadElement(decoder *xml.Decoder, start xml.StartElement) (*xmlNode, error) {
	node := &xmlNode{
		tag:   xmlTokenName(start.Name),
		attrs: make([]xmlAttr, 0, len(start.Attr)),
	}
	for _, attr := range start.Attr {
		node.attrs = append(node.attrs, xmlAttr{
			name:  xmlTokenName(attr.Name),
			value: attr.Value,
		})
	}

	for {
		token, err := decoder.RawToken()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("unexpected end of input")
			}
			return nil, err
		}

		switch tok := token.(type) {
		case xml.StartElement:
			child, err := xmlReadElement(decoder, tok)
			if err != nil {
				return nil, err
			}
			node.content = append(node.content, xmlContent{node: child})
		case xml.EndElement:
			if xmlTokenName(tok.Name) != xmlTokenName(start.Name) {
				return nil, fmt.Errorf("mismatched closing tag: expected %s, got %s", xmlTokenName(start.Name), xmlTokenName(tok.Name))
			}
			return node, nil
		case xml.CharData:
			text := string(tok)
			if strings.TrimSpace(text) == "" {
				continue
			}
			node.content = append(node.content, xmlContent{text: text})
		case xml.Comment, xml.Directive, xml.ProcInst:
			continue
		default:
			return nil, fmt.Errorf("unexpected token %T", token)
		}
	}
}

func xmlNodeToDoggo(node *xmlNode) types.Value {
	pairs := []types.Value{
		xmlTagKey, atom.NewKeyword(node.tag),
	}
	if len(node.attrs) > 0 {
		attrPairs := make([]types.Value, 0, len(node.attrs)*2)
		sort.Slice(node.attrs, func(i, j int) bool {
			return node.attrs[i].name < node.attrs[j].name
		})
		for _, attr := range node.attrs {
			attrPairs = append(attrPairs, atom.NewKeyword(attr.name), atom.NewString(attr.value))
		}
		pairs = append(pairs, xmlAttrsKey, coll.NewMapFromPairs(attrPairs))
	} else {
		pairs = append(pairs, xmlAttrsKey, atom.NilValue)
	}
	if len(node.content) > 0 {
		items := make([]types.Value, 0, len(node.content))
		for _, item := range node.content {
			if item.node != nil {
				items = append(items, xmlNodeToDoggo(item.node))
			} else {
				items = append(items, atom.NewString(item.text))
			}
		}
		pairs = append(pairs, xmlContentKey, coll.NewVector(items...))
	} else {
		pairs = append(pairs, xmlContentKey, atom.NilValue)
	}
	return coll.NewMapFromPairs(pairs)
}

func xmlDoggoToNode(value types.Value) (*xmlNode, error) {
	nodeMap, ok := value.(types.Map)
	if !ok {
		return nil, xmlInvalidArg("doggo.xml/write-string", "expected node map, got %s", value.Type())
	}

	tagValue := nodeMap.Get(xmlTagKey)
	tagName, err := xmlTagName(tagValue)
	if err != nil {
		return nil, xmlInvalidArg("doggo.xml/write-string", "field :tag %v", err)
	}

	node := &xmlNode{tag: tagName}

	attrsValue := nodeMap.Get(xmlAttrsKey)
	if !isNilArg(attrsValue) {
		attrMap, ok := attrsValue.(types.Map)
		if !ok {
			return nil, xmlInvalidArg("doggo.xml/write-string", "field :attrs must be a map or nil, got %s", attrsValue.Type())
		}
		attrSeqValue := attrMap.Seq()
		attrSeq, ok := attrSeqValue.(types.Seq)
		if !ok && attrSeqValue != nil {
			return nil, xmlInvalidArg("doggo.xml/write-string", "field :attrs must be seqable")
		}
		seenNames := make(map[string]struct{})
		for attrSeq != nil {
			entryValue := attrSeq.First()
			entry, ok := entryValue.(*types.MapEntry)
			if !ok {
				return nil, xmlInvalidArg("doggo.xml/write-string", "field :attrs must contain map entries")
			}
			name, err := xmlNamedValue(entry.Key)
			if err != nil {
				return nil, xmlInvalidArg("doggo.xml/write-string", "attribute key %v", err)
			}
			if _, duplicate := seenNames[name]; duplicate {
				return nil, xmlInvalidArg("doggo.xml/write-string", "duplicate attribute name %q", name)
			}
			seenNames[name] = struct{}{}
			attrValue := valueToString(entry.Value)
			if !xmlValidChars(attrValue) {
				return nil, xmlInvalidArg("doggo.xml/write-string", "attribute value for %q must contain only valid UTF-8 XML characters", name)
			}
			node.attrs = append(node.attrs, xmlAttr{name: name, value: attrValue})
			attrSeq = attrSeq.Next()
		}
		sort.Slice(node.attrs, func(i, j int) bool {
			return node.attrs[i].name < node.attrs[j].name
		})
	}

	contentValue := nodeMap.Get(xmlContentKey)
	if !isNilArg(contentValue) {
		seq, err := asSeq(contentValue, "doggo.xml/write-string")
		if err != nil {
			return nil, xmlInvalidArg("doggo.xml/write-string", "field :content must be sequential or nil, got %s", contentValue.Type())
		}
		for ; seq != nil; seq = seq.Next() {
			if err := sequenceEvalError(seq); err != nil {
				return nil, err
			}
			item := seq.First()
			if text, ok := item.(atom.String); ok {
				if !xmlValidChars(string(text)) {
					return nil, xmlInvalidArg("doggo.xml/write-string", "field :content text must contain only valid UTF-8 XML characters")
				}
				node.content = append(node.content, xmlContent{text: string(text)})
				continue
			}
			child, err := xmlDoggoToNode(item)
			if err != nil {
				return nil, err
			}
			node.content = append(node.content, xmlContent{node: child})
		}
	}

	return node, nil
}

func xmlNamedValue(value types.Value) (string, error) {
	var name string
	switch v := value.(type) {
	case atom.Keyword:
		name = string(v)
	case atom.String:
		name = string(v)
	case types.Symbol:
		name = string(v)
	case *types.SymbolWithMeta:
		if v != nil {
			name = v.Name
		}
	default:
		return "", fmt.Errorf("must be keyword, symbol, or string, got %s", valueTypeName(value))
	}
	if !xmlValidName(name) {
		return "", fmt.Errorf("must be a valid XML name, got %q", name)
	}
	return name, nil
}

func xmlTagName(value types.Value) (string, error) {
	switch value.(type) {
	case atom.Keyword, types.Symbol, *types.SymbolWithMeta:
		return xmlNamedValue(value)
	default:
		return "", fmt.Errorf("must be keyword or symbol, got %s", valueTypeName(value))
	}
}

func xmlTokenName(name xml.Name) string {
	switch {
	case name.Space == "" && name.Local == "":
		return ""
	case name.Space == "":
		return name.Local
	case name.Space == "xmlns" && name.Local == "":
		return "xmlns"
	case name.Space == "xmlns":
		return "xmlns:" + name.Local
	case name.Local == "":
		return name.Space
	default:
		return name.Space + ":" + name.Local
	}
}

func xmlWriteNode(sb *strings.Builder, node *xmlNode) error {
	sb.WriteByte('<')
	sb.WriteString(node.tag)
	for _, attr := range node.attrs {
		sb.WriteByte(' ')
		sb.WriteString(attr.name)
		sb.WriteString("='")
		xmlEscapeAttr(sb, attr.value)
		sb.WriteByte('\'')
	}

	if len(node.content) == 0 {
		sb.WriteString("/>")
		return nil
	}

	sb.WriteString(">\n")
	for i, item := range node.content {
		if item.node != nil {
			if err := xmlWriteNode(sb, item.node); err != nil {
				return err
			}
		} else {
			xmlEscapeText(sb, item.text)
		}
		if i < len(node.content) || len(node.content) > 0 {
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("</")
	sb.WriteString(node.tag)
	sb.WriteByte('>')
	return nil
}

func xmlEscapeText(sb *strings.Builder, value string) {
	for _, r := range value {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		default:
			sb.WriteRune(r)
		}
	}
}

func xmlEscapeAttr(sb *strings.Builder, value string) {
	for _, r := range value {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '\'':
			sb.WriteString("&apos;")
		case '"':
			sb.WriteString("&quot;")
		default:
			sb.WriteRune(r)
		}
	}
}
