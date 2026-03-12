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

func toType(id Ident) resolved.Type {
	s := string(id)
	if s == "" || s == "mixed" {
		return &resolved.Basic{Name: "mixed"}
	}
	if resolved.IsBasicName(s) {
		return &resolved.Basic{Name: s}
	}
	return &resolved.Named{Name: s}
}

// identFromType extracts the class name and optional generic template
// parameter from a resolved.Type, for passing into checkClassMember.
func identFromType(typ resolved.Type) (class, template Ident) {
	if g, ok := typ.(*resolved.Generic); ok {
		return Ident(g.Base.String()), Ident(g.Param.String())
	}
	return Ident(typ.String()), ""
}
