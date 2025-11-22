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
		typ := phptype.Named{Parts: strings.Split(name, `\`)}
		return &typ
	}
	return nil
}

func (p *parser) parseQualifiedName() string {
	var id strings.Builder
	if p.got(token.Backslash) {
		id.WriteRune('\\')
	}
	id.WriteString(p.tok.Text)
	if p.tok.Type.IsKeyword() {
		p.tok.Type = token.Ident
	}
	p.expect(token.Ident)
	for p.got(token.Backslash) {
		id.WriteString(`\` + p.tok.Text)
		if p.tok.Type.IsKeyword() {
			p.tok.Type = token.Ident
		}
		p.expect(token.Ident)
	}
	return id.String()
}

func (p *parser) fullyQualify(name string) string {
	if strings.HasPrefix(name, `\`) || isBasicType(name) {
		return name
	}
	if ns, rest, ok := strings.Cut(name, `\`); ok {
		if tr, ok := p.use[ns]; ok {
			return tr + `\` + rest
		}
	}
	if tr, ok := p.use[name]; ok {
		return tr
	}
	if p.namespace != "" {
		name = p.namespace + `\` + name
	}
	return name
}

func getClass(typ phptype.Type) string {
	switch typ := typ.(type) {
	case *phptype.Union:
		var opts []string
		for _, s := range typ.Types {
			c := getClass(s)
			// TODO: Fix this. The namespace isn't taken into account.
			if c == "\\stdClass" {
				return c
			}
			opts = append(opts, c)
		}
		return opts[0]
	case *phptype.Generic:
		return getClass(typ.Base)
	case *phptype.Nullable:
		return getClass(typ.Type)
	case *phptype.Named:
		name := strings.Join(typ.Parts, `\`)
		if typ.Global {
			return `\` + name
		}
		return name
	default:
		return fmt.Sprintf("<unsupported-%T>", typ)
	}
}

func isBasicType(typ string) bool {
	switch typ {
	case "void", "never", "static", "string", "int":
		return true
	default:
		return false
	}
}
