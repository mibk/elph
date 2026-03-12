package main

import (
	"strings"

	"mibk.dev/elph/resolved"
	"mibk.dev/phpfmt/token"
)

func (p *parser) tryParseType() resolved.Type {
	if p.got(token.Qmark) {
		return p.tryParseType() // nullable just unwraps
	}
	if p.got(token.Static) {
		return resolved.Static
	}
	if p.tok.Type == token.Backslash || p.tok.Type == token.Ident {
		name := p.parseQualifiedName()
		if p.got(token.BitOr) {
			// TODO: Support union types
			p.tryParseType() // just ignore
		}
		if name == "self" {
			return toType(p.thisClass)
		}
		if resolved.IsBasicName(name) {
			return resolved.TypeFromName(name)
		}
		name = p.fullyQualify(name)
		return &resolved.Named{Name: name}
	}
	return nil
}

func (p *parser) parseQualifiedName() string {
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
	return id.String()
}

func (p *parser) fullyQualify(id string) string {
	if strings.HasPrefix(id, `\`) {
		return id[1:]
	}
	if resolved.IsBasicName(id) {
		return id
	}
	if p.templateParam != "" && id == p.templateParam {
		return id
	}
	if ns, rest, ok := strings.Cut(id, `\`); ok {
		if tr, ok := p.use[ns]; ok {
			return tr + `\` + rest
		}
	}
	if tr, ok := p.use[id]; ok {
		return tr
	}
	if p.namespace != "" {
		id = p.namespace + `\` + id
	}
	return id
}

func toType(s string) resolved.Type {
	if s == "" || s == "mixed" {
		return resolved.Mixed
	}
	return resolved.TypeFromName(s)
}

// identFromType extracts the class name and optional generic template
// parameter from a resolved.Type, for passing into checkClassMember.
func identFromType(typ resolved.Type) (class string, template resolved.Type) {
	if g, ok := typ.(*resolved.Generic); ok {
		return g.Base.String(), g.Param
	}
	return typ.String(), nil
}
