package main

import (
	"fmt"
	"io"
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
	use       map[string]string
	thisClass string
}

func Parse(r io.Reader, php74Compat bool) (*File, error) {
	p := &parser{scan: token.NewScanner(r, php74Compat)}
	p.use = make(map[string]string)
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

func (p *parser) errorf(format string, args ...any) {
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

	file.Scope = p.parseScope(token.OpenTag)
	return file
}

func (p *parser) parseScope(open token.Type) *scope {
	s := &scope{Open: open}
	sep := token.Semicolon

	switch open {
	default:
		panic(fmt.Sprintf("unknown pair for %v", open))
	case token.OpenTag:
		s.close = token.EOF
	case token.Lbrace:
		s.close = token.Rbrace
	case token.Lparen:
		s.close = token.Rparen
	case token.Lbrack:
		s.close = token.Rbrack
		sep = token.Comma
	}

	for {
		stmt := p.parseStmt(sep)
		p.got(sep)
		if len(stmt.Nodes) > 0 {
			s.Stmts = append(s.Stmts, stmt)
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

func (p *parser) parseStmt(separators ...token.Type) (s *Stmt) {
	s = new(Stmt)
	var docComment string
	for {
		// TODO: make these keywords indents: token.Arrow, token.DoubleColon
		switch typ := p.tok.Type; typ {
		case token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			return s
		case token.OpenTag:
			p.next()
			return s
		case token.Comment:
			pos := p.tok.Pos
			v, ok := strings.CutPrefix(p.tok.Text, "#debugType")
			p.next()
			if !ok {
				break
			}
			s.Nodes = append(s.Nodes, &Debug{Var: strings.TrimSpace(v), Pos: pos})
		case token.DocComment:
			docComment = p.tok.Text
			// log.Println(docComment)
			p.next()
		case token.Namespace:
			p.next()
			p.namespace = p.parseFQN()
			// log.Println("NAMESPACE", p.namespace)
		case token.Use:
			p.next()
			use := p.parseFQN()
			// log.Println("USE", use)
			last := use
			if i := strings.LastIndexByte(last, '\\'); i >= 0 {
				last = last[i+1:]
			}
			p.use[last] = use
		case token.Class:
			if c := p.parseClass(docComment); c != nil {
				s.Nodes = append(s.Nodes, c)
			}
		case token.Private, token.Protected, token.Public:
			p.parseMember(docComment)
		case token.Lparen:
			p.next()
			sub := p.parseScope(typ)
			s.Nodes = append(s.Nodes, sub)
		case token.Lbrace, token.Lbrack:
			p.next()
			sub := p.parseScope(typ)
			s.Nodes = append(s.Nodes, sub)
			if typ == token.Lbrace {
				return s
			}
		case token.Var:
			e := p.parseExpr()
			s.Nodes = append(s.Nodes, e)
		default:
			if slices.Contains(separators, typ) {
				return s
			}
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

// TODO: Is this global var necessary/convenient?
var world = make(map[string]*Class)

func (p *parser) parseClass(doc string) *Class {
	p.expect(token.Class)
	name := p.tok
	if !p.got(token.Ident) {
		// TODO: anonymous class
		return nil
	}
	p.thisClass = name.Text
	if p.namespace != "" {
		p.thisClass = p.namespace + "\\" + p.thisClass
	}
	// log.Println("CLASS", p.thisClass)

	c := world[p.thisClass]
	if c != nil {
		// TODO: it is parsed twice
		p.errorf("class %v already defined", p.thisClass)
		return nil
	}
	c = &Class{Name: p.thisClass, Members: make(map[string]*Member)}
	world[p.thisClass] = c

	if p.got(token.Extends) {
		e := p.parseFQN()
		// TODO: dedup
		if ns, rest, ok := strings.Cut(e, "\\"); ok {
			if tr, ok := p.use[ns]; ok {
				e = tr + "\\" + rest
			}
		} else {
			e = p.namespace + `\` + e
		}

		c.Extends = e
		// log.Println("EXTENDS", c.Extends)
	}

	if doc != "" {
		b, err := phpdoc.Parse(strings.NewReader(doc))
		if err != nil {
			p.errorf("parsing doc %q: %v", doc, err)
			return nil
		}
		for _, line := range b.Lines {
			if tag, ok := line.(*phpdoc.PropertyTag); ok {
				m := &Member{
					Name:  strings.TrimPrefix(tag.Var, "$"),
					Type:  tag.Type,
					Class: p.getClass(tag.Type),
				}
				c.Members[m.Name] = m
			}
		}
	}

	return c
}

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
		// TODO: not true
		typ = &phptype.Named{Parts: []string{"void"}}
	}

	c := world[p.thisClass]
	if c == nil {
		p.errorf("unknown class %v", p.thisClass)
		return
	}

	name := strings.TrimPrefix(def.Text, "$")
	if _, ok := c.Members[name]; ok {
		p.errorf("member %v already defined for %v", name, c.Name)
	}
	class := p.getClass(typ)
	c.Members[name] = &Member{Name: name, Type: typ, Class: class}
	// log.Printf("DEF %v %v %T", c.Name, def, typ)
}

func (p *parser) parseExpr() Expr {
	e := p.parseVarExpr()
	if p.got(token.Assign) {
		r := p.parseExpr()
		e = &AssignExpr{e, r}
	}
	return e
}

func (p *parser) parseVarExpr() Expr {
	var x Expr = &VarExpr{Name: p.tok.Text}
	if !p.got(token.Var) {
		return &VarExpr{Name: "<not-a-class>"}
	}

	for p.got(token.Arrow) {
		x = p.parseMemberAccess(x)
	}
	return x
}

func (p *parser) parseMemberAccess(x Expr) Expr {
	a := &MemberAccess{Rcvr: x, Name: p.tok.Text, Pos: p.tok.Pos}
	if p.got(token.Ident) {
		return a
	}
	return x
}

func (p *parser) getClass(typ phptype.Type) string {
	class := getClass(typ)
	if class == "self" {
		return p.thisClass
	}
	if ns, rest, ok := strings.Cut(class, "\\"); ok {
		if tr, ok := p.use[ns]; ok {
			class = tr + "\\" + rest
		}
	} else if p.namespace != "" && !isBasicType(class) {
		// TODO: Even if class has \,
		// it still should belong under namespace.
		class = p.namespace + `\` + class
	}
	return class
}

func getClass(typ phptype.Type) string {
	switch typ := typ.(type) {
	case *phptype.Generic:
		return getClass(typ.Base)
	case *phptype.Nullable:
		return getClass(typ.Type)
	case *phptype.Named:
		return strings.Join(typ.Parts, "\\")
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
