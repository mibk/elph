package main

import (
	"fmt"
	"strings"

	"mibk.dev/phpfmt/phpdoc/phptype"
	"mibk.dev/phpfmt/token"
)

func (p *parser) tryParseType() phptype.Type {
	if p.tok.Type == token.Backslash || p.tok.Type == token.Ident {
		name := p.parseQualifiedName()
		name = p.fullyQualify(name)
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
	p.expect(token.Ident)
	for p.got(token.Backslash) {
		id.WriteString(`\` + p.tok.Text)
		p.expect(token.Ident)
	}
	return id.String()
}

func (p *parser) fullyQualify(name string) string {
	if strings.HasPrefix(name, `\`) {
		return name
	}
	if ns, rest, ok := strings.Cut(name, `\`); ok {
		if tr, ok := p.use[ns]; ok {
			name = tr + `\` + rest
		}
	} else if p.namespace != "" && !isBasicType(name) {
		// TODO: Even if name has \,
		// it still should belong under namespace.
		name = p.namespace + `\` + name
	}
	return name
}

func getClass(typ phptype.Type) string {
	switch typ := typ.(type) {
	case *phptype.Generic:
		return getClass(typ.Base)
	case *phptype.Nullable:
		return getClass(typ.Type)
	case *phptype.Named:
		return strings.Join(typ.Parts, `\`)
	default:
		return fmt.Sprintf("%T", typ)
	}
}

func isBasicType(typ string) bool {
	switch typ {
	case "void", "never", "string":
		return true
	default:
		return false
	}
}
