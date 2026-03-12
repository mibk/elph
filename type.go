package main

import (
	"strings"

	"mibk.dev/elph/resolved"
	"mibk.dev/phpfmt/phpdoc/phptype"
	"mibk.dev/phpfmt/token"
)

func (p *parser) tryParseType() phptype.Type {
	if p.got(token.Qmark) {
		typ := p.tryParseType()
		if typ == nil {
			p.errorf("expecting type def; found %s", p.tok)
			return nil
		}
		return &phptype.Nullable{Type: typ}
	}
	if p.got(token.Static) {
		return &phptype.Named{Parts: []string{"static"}}
	}
	if p.tok.Type == token.Backslash || p.tok.Type == token.Ident {
		name := p.parseQualifiedName()
		typ := phptype.Named{Parts: strings.Split(string(name), `\`)}
		if p.got(token.BitOr) {
			// TODO: Support union types
			p.tryParseType() // just ignore
		}
		return &typ
	}
	return nil
}

type Ident string

func (id Ident) String() string {
	if base, typ, ok := strings.Cut(string(id), "<>"); ok {
		return base + "<" + typ + ">"
	}
	return string(id)
}

func (p *parser) parseQualifiedName() Ident {
	var id strings.Builder
	if p.got(token.Backslash) {
		id.WriteRune('\\')
	}
	// TODO: Do not allow { in all contexts.
	for p.tok.Type != token.Lbrace {
		id.WriteString(p.tok.Text)
		if p.tok.Type.IsReserved() {
			p.tok.Type = token.Ident
		}
		p.expect(token.Ident)
		if p.got(token.Backslash) {
			id.WriteRune('\\')
			continue
		}
		break
	}
	return Ident(id.String())
}

func (p *parser) fullyQualify(id Ident) Ident {
	name := string(id)
	if strings.HasPrefix(name, `\`) {
		return Ident(name[1:])
	}
	if isBasicType(id) {
		return id
	}
	if p.templateParam != "" && name == p.templateParam {
		return id
	}
	if ns, rest, ok := strings.Cut(name, `\`); ok {
		if tr, ok := p.use[ns]; ok {
			return tr + Ident(`\`+rest)
		}
	}
	if tr, ok := p.use[name]; ok {
		return tr
	}
	if p.namespace != "" {
		id = p.namespace + `\` + id
	}
	return id
}

func isBasicType(typ Ident) bool {
	return resolved.IsBasicName(string(typ))
}

// toType converts a string-encoded Ident into a structured resolved.Type.
func toType(id Ident) resolved.Type {
	s := string(id)
	if s == "" || s == "mixed" {
		return &resolved.Basic{Name: "mixed"}
	}
	// Union
	if strings.Contains(s, "|") {
		parts := strings.Split(s, "|")
		types := make([]resolved.Type, len(parts))
		for i, p := range parts {
			types[i] = toType(Ident(p))
		}
		return resolved.NewUnion(types...)
	}
	// Array
	if elem, ok := strings.CutPrefix(s, "[]"); ok {
		return &resolved.Array{Elem: toType(Ident(elem))}
	}
	// Generic
	if base, param, ok := strings.Cut(s, "<>"); ok {
		return &resolved.Generic{
			Base:  toType(Ident(base)),
			Param: toType(Ident(param)),
		}
	}
	// Basic
	if resolved.IsBasicName(s) {
		return &resolved.Basic{Name: s}
	}
	// Named class
	return &resolved.Named{Name: s}
}

// toIdent converts a structured resolved.Type back to a string-encoded Ident.
func toIdent(typ resolved.Type) Ident {
	switch t := typ.(type) {
	case *resolved.Named:
		return Ident(t.Name)
	case *resolved.Basic:
		return Ident(t.Name)
	case *resolved.TypeVar:
		return Ident(t.Name)
	case *resolved.Union:
		parts := make([]string, len(t.Types))
		for i, m := range t.Types {
			parts[i] = string(toIdent(m))
		}
		return Ident(strings.Join(parts, "|"))
	case *resolved.Array:
		return "[]" + toIdent(t.Elem)
	case *resolved.Generic:
		return toIdent(t.Base) + "<>" + toIdent(t.Param)
	default:
		return "mixed"
	}
}
