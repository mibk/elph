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
	typ := p.tryParseSingleType()
	if typ == nil || !p.got(token.BitOr) {
		return typ
	}
	types := []resolved.Type{typ}
	for {
		if t := p.tryParseSingleType(); t != nil {
			types = append(types, t)
		}
		if !p.got(token.BitOr) {
			break
		}
	}
	var filtered []resolved.Type
	for _, t := range types {
		if t == resolved.Mixed {
			return resolved.Mixed
		}
		if n, ok := t.(*resolved.Named); ok && n.Name == "stdClass" {
			return t
		}
		if t == resolved.Null {
			continue
		}
		filtered = append(filtered, t)
	}
	if len(filtered) == 0 {
		return resolved.Mixed
	}
	return resolved.NewUnion(filtered...)
}

func (p *parser) tryParseSingleType() resolved.Type {
	if p.got(token.Static) {
		return resolved.Static
	}
	if p.tok.Type == token.Backslash || p.tok.Type == token.Ident {
		name := p.parseQualifiedName()
		if name == "self" {
			return toType(p.thisClass)
		}
		if resolved.IsBuiltinName(name) {
			return resolved.TypeFromName(name)
		}
		name = p.fullyQualify(name)
		return resolved.TypeFromName(name)
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
	if resolved.IsBuiltinName(id) {
		return id
	}
	if p.templateParam != nil && id == p.templateParam.Name {
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
