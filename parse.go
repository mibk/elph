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
			p.namespace = p.parseQualifiedName()
			// log.Println("NAMESPACE", p.namespace)
		case token.Use:
			p.next()
			use := p.parseQualifiedName()
			// log.Println("USE", use)
			last := use
			if i := strings.LastIndexByte(last, '\\'); i >= 0 {
				last = last[i+1:]
			}
			p.use[last] = use
		case token.Class:
			doc := docComment
			if c := p.parseClass(); c != nil {
				s.Nodes = append(s.Nodes, c)
				sub := p.parseScope(token.Lbrace)
				s.Nodes = append(s.Nodes, sub)

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
			}
		case token.Trait:
			if c := p.parseTrait(docComment); c != nil {
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

// TODO: Is this global var necessary/convenient?
var universe = make(map[string]typeDecl)

func (p *parser) parseClass() *Class {
	p.expect(token.Class)
	name := p.tok
	if !p.got(token.Ident) {
		// TODO: anonymous class
		return nil
	}
	p.thisClass = name.Text
	if p.namespace != "" {
		p.thisClass = p.namespace + `\` + p.thisClass
	}
	// log.Println("CLASS", p.thisClass)

	if _, ok := universe[p.thisClass]; ok {
		p.errorf("type %v already defined", p.thisClass)
		return nil
	}
	c := &Class{Name: p.thisClass, Members: make(map[string]*Member)}
	universe[p.thisClass] = c

	if p.got(token.Extends) {
		e := p.parseQualifiedName()
		c.Extends = p.fullyQualify(e)
		// log.Println("EXTENDS", c.Extends)
	}

	// TODO: Choose a different aproach to skip tokens unil '{'?
	if p.got(token.Implements) {
		for {
			p.parseQualifiedName() // ignore these
			if !p.got(token.Comma) {
				break
			}
		}
	}

	p.expect(token.Lbrace)
	for p.got(token.Use) {
		use := p.parseQualifiedName()
		use = p.fullyQualify(use)
		c.Traits = append(c.Traits, use)
		p.expect(token.Semicolon)
	}

	return c
}

func (p *parser) parseTrait(doc string) *Trait {
	p.expect(token.Trait)
	name := p.tok
	p.expect(token.Ident)
	p.thisClass = name.Text
	if p.namespace != "" {
		p.thisClass = p.namespace + `\` + p.thisClass
	}
	// log.Println("TRAIT", p.thisClass)

	if _, ok := universe[p.thisClass]; ok {
		p.errorf("type %v already defined", p.thisClass)
		return nil
	}
	t := &Trait{Name: p.thisClass, Members: make(map[string]*Member)}
	universe[p.thisClass] = t

	// TODO: Doc comment.
	return t
}

func (p *parser) parseMember(doc string) {
	p.next()

	if p.got(token.Static) {
		// TODO: Skip these for now.
		return
	}

	if p.got(token.Function) {
		p.parseFunction(doc)
	} else {
		p.parseProperty(doc)
	}
}

func (p *parser) parseFunction(doc string) {
	// We don't care whether the function returns a reference, or not.
	p.consume(token.BitAnd)

	def := p.tok
	p.expect(token.Ident)

	var typ phptype.Type
	if doc != "" {
		b, err := phpdoc.Parse(strings.NewReader(doc))
		if err != nil {
			p.errorf("parsing doc %q: %v", doc, err)
			return
		}
		for _, line := range b.Lines {
			if tag, ok := line.(*phpdoc.ReturnTag); ok {
				typ = tag.Type
				break
			}
		}
	}
	if typ == nil {
		// TODO: not true
		typ = &phptype.Named{Parts: []string{"void"}}
	}

	c, _ := universe[p.thisClass]
	if c == nil {
		p.errorf("unknown class %v", p.thisClass)
		return
	}

	class := p.getClass(typ)
	m := Member{Name: def.Text, Type: typ, Class: class}
	if err := c.addMember(&m); err != nil {
		p.errorf("%v", err)
	}
}

func (p *parser) parseProperty(doc string) {
	typ := p.tryParseType()

	def := p.tok
	if !p.got(token.Var) {
		return
	}

	if doc != "" {
		b, err := phpdoc.Parse(strings.NewReader(doc))
		if err != nil {
			p.errorf("parsing doc %q: %v", doc, err)
			return
		}
		for _, line := range b.Lines {
			if tag, ok := line.(*phpdoc.VarTag); ok {
				typ = tag.Type
				break
			}
		}
	}
	if typ == nil {
		// TODO: again, not true
		typ = &phptype.Named{Parts: []string{"mixed"}}
	}

	c, _ := universe[p.thisClass]
	if c == nil {
		p.errorf("unknown class %v", p.thisClass)
		return
	}

	name := strings.TrimPrefix(def.Text, "$")
	class := p.getClass(typ)
	m := Member{Name: name, Type: typ, Class: class}
	if err := c.addMember(&m); err != nil {
		p.errorf("%v", err)
	}
}

func (p *parser) parseExpr() Expr {
	e := p.parseVarExpr()
	if p.got(token.Assign) {
		var v Expr
		if p.got(token.New) {
			v = p.parseNewInstance()
		} else {
			v = p.parseExpr()
		}
		e = &AssignExpr{e, v}
	}
	return e
}

func (p *parser) parseNewInstance() Expr {
	name := p.parseQualifiedName()
	if name == "" {
		p.expect(token.Ident)
		return nil
	}
	name = p.fullyQualify(name)
	return &NewInstance{Class: name}
}

func (p *parser) parseVarExpr() Expr {
	var x Expr = &VarExpr{Name: p.tok.Text}
	if !p.got(token.Var) {
		return &VarExpr{Name: "<not-a-class>"}
	}

	for p.got(token.Arrow) || p.got(token.QmarkArrow) {
		x = p.parseMemberAccess(x)
	}
	return x
}

func (p *parser) parseMemberAccess(x Expr) Expr {
	a := &MemberAccess{Rcvr: x, NamePos: p.tok.Pos, Name: p.tok.Text}
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
	class = p.fullyQualify(class)
	return class
}
