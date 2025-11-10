package main

import (
	"cmp"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"

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
		case token.Arrow:
			log.Println(s.kind, typ)
			fallthrough
		case token.Declare,
			token.Namespace,
			token.Class, token.Interface, token.Trait, token.Enum,
			token.Function, token.Fn,
			token.If, token.Else, token.Switch, token.Match,
			token.For, token.Foreach, token.Do, token.While,
			token.Try, token.Catch, token.Finally,
			token.Hash, token.DoubleColon:
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
		default:
			if slices.Contains(separators, typ) {
				return s
			}
			s.nodes = append(s.nodes, p.tok)
			p.next()
		}
	}
}
