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

func (id Ident) unslash() Ident {
	// TODO: Do we need this method?
	return Ident(strings.TrimPrefix(string(id), `\`))
}

func (p *parser) parseQualifiedName() Ident {
	var id strings.Builder
	if p.got(token.Backslash) {
		id.WriteRune('\\')
	}
	// TODO: Do not allow { in all contexts.
	for p.tok.Type != token.Lbrace {
		id.WriteString(p.tok.Text)
		if p.tok.Type.IsKeyword() {
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
	if strings.HasPrefix(name, `\`) || isBasicType(id) {
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
		var opts []Ident
		for _, s := range typ.Types {
			c := getClass(s)
			// TODO: Fix this. The namespace isn't taken into account.
			if c == "\\stdClass" {
				return c
			}
			opts = append(opts, c)
		}
		// TODO: Not just the first one, I guess.
		return opts[0]
	case *phptype.Generic:
		return getClass(typ.Base) + "<>T"
	case *phptype.Array:
		// If there's a method on array,
		// it's not actually an array.
		// TODO: Add proper support.
		return "\\stdClass"
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
	default:
		return Ident(fmt.Sprintf("<unsupported-%T>", typ))
	}
}

func isBasicType(typ Ident) bool {
	switch typ {
	case TemplateParam:
		// TODO: This is ugly.
		return true
	case "void", "never", "static", "mixed", "string", "int":
		return true
	default:
		return false
	}
}
