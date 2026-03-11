package main

import (
	"fmt"
	"strings"

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
	if strings.Contains(string(id), "|") {
		parts := strings.Split(string(id), "|")
		for i, part := range parts {
			parts[i] = string(p.fullyQualify(Ident(part)))
		}
		return Ident(strings.Join(parts, "|"))
	}
	if elem, ok := strings.CutPrefix(string(id), "[]"); ok {
		return "[]" + p.fullyQualify(Ident(elem))
	}
	if base, typ, ok := strings.Cut(string(id), "<>"); ok {
		return p.fullyQualify(Ident(base)) + "<>" + p.fullyQualify(Ident(typ))
	}
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

func getClass(typ phptype.Type) Ident {
	switch typ := typ.(type) {
	case nil:
		return "mixed"
	case *phptype.Union:
		var classes []Ident
		for _, s := range typ.Types {
			c := getClass(s)
			if c == `\stdClass` {
				return c
			}
			if c == "mixed" {
				return c
			}
			if strings.ToLower(string(c)) == "null" {
				continue
			}
			classes = append(classes, c)
		}
		if len(classes) == 0 {
			return "mixed"
		}
		if len(classes) == 1 {
			return classes[0]
		}
		var parts []string
		for _, c := range classes {
			parts = append(parts, string(c))
		}
		return Ident(strings.Join(parts, "|"))
	case *phptype.Generic:
		id := getClass(typ.Base) + "<>"
		if len(typ.TypeParams) > 1 {
			return id + "MANY-NOT-SUPPORTED"
		}
		return id + getClass(typ.TypeParams[0])
	case *phptype.Array:
		return "[]" + getClass(typ.Elem)
	case *phptype.ArrayShape:
		return `\stdClass`
	case *phptype.ObjectShape:
		return `\stdClass`
	case *phptype.Nullable:
		return getClass(typ.Type)
	case *phptype.Named:
		name := strings.Join(typ.Parts, `\`)
		if typ.Global {
			return Ident(`\` + name)
		}
		return Ident(name)
	case *phptype.This:
		return "static"
	case *phptype.Conditional:
		return getClass(typ.True)
	default:
		return Ident(fmt.Sprintf("<unsupported-%T>", typ))
	}
}

func arrayElemType(id Ident) (elem Ident, ok bool) {
	if strings.Contains(string(id), "|") {
		parts := strings.Split(string(id), "|")
		var elems []string
		for _, p := range parts {
			if e, ok := strings.CutPrefix(p, "[]"); ok {
				elems = append(elems, e)
			}
		}
		if len(elems) == 0 {
			return "", false
		}
		return Ident(strings.Join(elems, "|")), true
	}
	if e, ok := strings.CutPrefix(string(id), "[]"); ok {
		return Ident(e), true
	}
	return "", false
}

func isBasicType(typ Ident) bool {
	switch typ {
	case "void", "never", "self", "static", "parent",
		"mixed", "string", "int", "float", "bool", "true", "false",
		"object", "array", "callable", "resource":
		return true
	default:
		return false
	}
}
