package main

import (
	"cmp"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"

	"mibk.dev/phpfmt/phpdoc"
	"mibk.dev/phpfmt/phpdoc/phptype"
	"mibk.dev/phpfmt/token"
)

// SyntaxError records an error and the position it occurred on.
type SyntaxError struct {
	Line, Column int
	Err          error
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line:%d:%d: %v", e.Line, e.Column, e.Err)
}

type parser struct {
	scan *token.Scanner

	err error
	tok token.Token
	alt *token.Token // TODO: rm?

	namespace string
	thisClass string
}

func Parse(r io.Reader, php74Compat bool) (*File, error) {
	p := &parser{scan: token.NewScanner(r, php74Compat)}
	p.next() // init
	doc := p.parseFile()
	if p.err != nil {
		return nil, p.err
	}
	return doc, nil
}

func (p *parser) next0() {
	if p.tok.Type == token.EOF {
		return
	}
	if p.alt != nil {
		p.tok, p.alt = *p.alt, nil
		return
	}
	p.tok = p.scan.Next()
}

func (p *parser) next() {
	// TODO: p.prev = p.tok
	p.next0()
	p.consume(token.Whitespace)
}

func (p *parser) expect(typ token.Type) {
	if p.tok.Type != typ {
		p.errorf("expecting %v, found %v", typ, p.tok)
	}
	p.next()
}

func (p *parser) got(typ token.Type) bool {
	if p.tok.Type == typ {
		p.next()
		return true
	}
	return false
}

func (p *parser) consume(types ...token.Type) {
	if len(types) == 0 {
		panic("no token types to consume provided")
	}

	for ; len(types) > 0; types = types[1:] {
		if p.tok.Type == types[0] {
			p.next0()
		}
	}
}

func (p *parser) errorf(format string, args ...interface{}) {
	if p.err == nil {
		p.tok.Type = token.EOF
		se := &SyntaxError{Err: fmt.Errorf(format, args...)}
		se.Line, se.Column = p.tok.Pos.Line, p.tok.Pos.Column
		p.err = se
	}
}

func (p *parser) parseFile() *File {
	file := new(File)
	p.consume(token.InlineHTML)
	p.expect(token.OpenTag)

	file.scope = p.parseScope(token.Illegal, token.OpenTag)
	return file
}

func (p *parser) parseScope(kind, open token.Type) (s *scope) {
	s = &scope{kind: kind, open: open}

	sep := token.Semicolon

	switch open {
	default:
		panic(fmt.Sprintf("unknown pair for %v", open))
	case token.OpenTag:
		s.close = token.EOF
	case token.Lbrace:
		s.close = token.Rbrace
		if kind == token.Match {
			sep = token.Comma
		}
	case token.Lparen:
		s.close = token.Rparen
		switch kind {
		case token.Ident, token.Var:
			kind = token.Illegal
			fallthrough
		case token.Function:
			sep = token.Comma
		}
	case token.Lbrack:
		s.close = token.Rbrack
		sep = token.Comma
	}

	for {
		stmt := p.parseStmt(sep)
		if tsep := p.tok; p.got(sep) {
			stmt.nodes = append(stmt.nodes, tsep)
		}
		if len(stmt.nodes) > 0 {
			if p.tok.Type == token.Whitespace && !strings.Contains(p.tok.Text, "\n") {
				p.next()
			}
			s.nodes = append(s.nodes, stmt)
		}

		if s.open == token.Lparen && s.kind == token.Function {
			stmt.kind = token.Function
		} else if s.open == token.Lbrace && s.kind == token.Class {
			stmt.kind = token.Class
		}

		switch typ := p.tok.Type; typ {
		case s.close:
			p.next()
			return s
		case token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			p.errorf("unexpected %v", typ)
			return s
		}
	}
	return s
}

func (p *parser) parseStmt(separators ...token.Type) (s *stmt) {
	s = new(stmt)
	nextScope := token.OpenTag
	var docComment string
	for {
		// TODO: make these keywords indents: token.Arrow, token.DoubleColon
		switch typ := p.tok.Type; typ {
		case token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			if len(s.nodes) > 0 {
				if tok, ok := s.nodes[len(s.nodes)-1].(token.Token); ok && tok.Type == token.Whitespace {
					s.nodes = s.nodes[:len(s.nodes)-1]
				}
			}
			return s
		case token.OpenTag:
			s.nodes = append(s.nodes, p.tok)
			p.next()
			return s
		case token.DocComment:
			docComment = p.tok.Text
			// log.Println(docComment)
			p.next()
		case token.Namespace:
			p.next()
			p.namespace = p.parseFQN()
			log.Println("NAMESPACE", p.namespace)
		case token.Class:
			p.next()
			if name := p.tok; p.got(token.Ident) {
				p.thisClass = name.Text
				if p.namespace != "" {
					p.thisClass = p.namespace + "\\" + p.thisClass
				}
				log.Println("CLASS", p.thisClass)
			}
			fallthrough
		case token.Private, token.Protected, token.Public:
			p.parseMember(docComment)
		case token.Declare,
			token.Interface, token.Trait, token.Enum,
			token.Function, token.Fn,
			token.If, token.Else, token.Switch, token.Match,
			token.For, token.Foreach, token.Do, token.While,
			token.Try, token.Catch, token.Finally,
			token.Hash, token.Arrow, token.DoubleColon:
			nextScope = typ
			s.kind = cmp.Or(s.kind, typ)
			s.nodes = append(s.nodes, p.tok)
			p.next()
		case token.Lparen:
			scope := nextScope
			for _, v := range slices.Backward(s.nodes) {
				switch tok, _ := v.(token.Token); tok.Type {
				case token.Whitespace:
					continue
				case token.Echo, token.Print, token.Static:
					scope = token.Ident
				case token.Ident, token.Var:
					if nextScope != token.Function {
						scope = tok.Type
					}
				case token.Class, token.Function:
					// Let's use something that always places { on the same line.
					nextScope = token.Fn
				}
				break
			}
			p.next()
			sub := p.parseScope(scope, typ)
			s.nodes = append(s.nodes, sub)
		case token.Lbrace, token.Lbrack:
			s.kind = cmp.Or(s.kind, typ)
			p.next()
			sub := p.parseScope(nextScope, typ)
			s.nodes = append(s.nodes, sub)
			if typ == token.Lbrace {
				return s
			} else if typ == token.Lbrack && s.kind == token.Hash {
				return s
			}
		case token.Var:
			if p.tok.Text == "$this" {
				p.parseExpr()
				break
			}
			fallthrough
		default:
			if slices.Contains(separators, typ) {
				return s
			}
			s.nodes = append(s.nodes, p.tok)
			p.next()
			docComment = ""
		}
	}
}

func (p *parser) parseFQN() string {
	var id strings.Builder
	id.WriteString(p.tok.Text)
	p.expect(token.Ident)
	for p.got(token.Backslash) {
		id.WriteString("\\" + p.tok.Text)
		p.expect(token.Ident)
	}
	return id.String()
}

var world = make(map[string]*Class)

func (p *parser) parseMember(doc string) {
	p.next()

	p.consume(token.Static)
	searchFn := p.got(token.Function)
	if searchFn {
		p.consume(token.BitAnd)
	}

	def := p.tok
	if searchFn && !p.got(token.Ident) {
		return
	} else if !searchFn && !p.got(token.Var) {
		return
	}

	var typ phptype.Type
	if doc != "" {
		b, err := phpdoc.Parse(strings.NewReader(doc))
		if err != nil {
			p.errorf("parsing doc %q: %v", doc, err)
			return
		}
		for _, line := range b.Lines {
			if tag, ok := line.(*phpdoc.ReturnTag); ok && searchFn {
				typ = tag.Type
				break
			} else if tag, ok := line.(*phpdoc.VarTag); ok {
				typ = tag.Type
				break
			}
		}
	}
	if typ == nil {
		// log.Printf("cannot deduce type for member `%v`", def.Text)
		return
	}

	c := world[p.thisClass]
	if c == nil {
		c = &Class{Name: p.thisClass, Members: make(map[string]*Member)}
		world[p.thisClass] = c
	}

	name := strings.TrimPrefix(def.Text, "$")
	if _, ok := c.Members[name]; ok {
		p.errorf("member %v already defined for %v", name, c.Name)
	}
	c.Members[name] = &Member{Name: name, Type: typ}
	log.Printf("DEF %v %v %T", c.Name, def, typ)
}

func (p *parser) parseExpr() {
	x := p.tok.Text
	p.next()

	if x == "$this" {
		x = p.thisClass
	}

	for p.got(token.Arrow) {
		if tok := p.tok; p.got(token.Ident) {
			c, ok := world[x]
			if !ok {
				log.Printf("class `%v` not found", x)
				return
			}
			m, ok := c.Members[tok.Text]
			if !ok {
				log.Printf("member `%v` not found", tok.Text)
				return
			}

			x = getClass(m.Type)
		}
	}
}

func getClass(typ phptype.Type) string {
	switch typ := typ.(type) {
	case *phptype.Generic:
		return getClass(typ.Base)
	case *phptype.Named:
		return strings.Join(typ.Parts, "\\")
	default:
		return fmt.Sprintf("%T", typ)
	}
}
