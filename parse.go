package main

import (
	"fmt"
	"io"
	"os"
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

	filename string
	warnOut  io.Writer

	err error
	tok token.Token
	alt *token.Token // TODO: rm?

	namespace string
	use       map[string]string
	thisClass string
	nextClass string
	params    []Param
}

func Parse(r io.Reader, filename string, php74Compat bool) (*File, error) {
	return parsePHP(r, filename, php74Compat, os.Stderr)
}

func parsePHP(r io.Reader, filename string, php74Compat bool, warnOut io.Writer) (*File, error) {
	p := &parser{scan: token.NewScanner(r, php74Compat), filename: filename, warnOut: warnOut}
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
	p.consume(token.Comment, token.Whitespace)
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
		s.Params = p.params
		p.params = nil
		backup := p.thisClass
		p.thisClass = p.nextClass
		defer func() { p.thisClass = backup }()
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
	var docComment token.Token
	afterFunc := false
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
			docComment = p.tok
			p.next()
		case token.Namespace:
			p.next()
			p.namespace = p.parseQualifiedName()
			// log.Println("NAMESPACE", p.namespace)
		case token.Use:
			p.next()
			if afterFunc || p.got(token.Function) || p.got(token.Const) {
				continue
			}
			for _, use := range p.parseUseStmt() {
				p.use[use.Alias] = use.Namespace
			}
		case token.Abstract:
			p.next()
		case token.Class:
			doc := docComment
			if c := p.parseClass(); c != nil {
				s.Nodes = append(s.Nodes, c)
				sub := p.parseScope(token.Lbrace)
				s.Nodes = append(s.Nodes, sub)

				if b := p.parsePHPDoc(doc); b != nil {
					for _, line := range b.Lines {
						if tag, ok := line.(*phpdoc.PropertyTag); ok {
							m := &Property{
								Name:  strings.TrimPrefix(tag.Var, "$"),
								Type:  tag.Type,
								Class: p.getClass(c.Name, tag.Type),
							}
							c.Properties[m.Name] = m
						}
					}
				}
			}
		case token.Trait:
			if c := p.parseTrait(docComment); c != nil {
				s.Nodes = append(s.Nodes, c)
			}
		case token.Interface:
			if c := p.parseInterface(); c != nil {
				s.Nodes = append(s.Nodes, c)
			}
		case token.Private, token.Protected, token.Public:
			p.parseMember(docComment)
		case token.Function:
			p.next()
			p.parseFunction(docComment)
			// Disarm parsing 'use' stmt after 'function' for now.
			afterFunc = true
		case token.Foreach:
			if f := p.parseForeach(); f != nil {
				s.Nodes = append(s.Nodes, f)
			}
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
		case token.New:
			p.next()
			p.parseNewInstance()
		case token.Arrow, token.QmarkArrow, token.DoubleColon:
			p.next()
			// Keywords after :: are idents.
			if p.tok.Type.IsKeyword() {
				p.tok.Type = token.Ident
			}
		default:
			if slices.Contains(separators, typ) {
				return s
			}
			p.next()
			docComment = token.Token{Type: token.Illegal}
		}
	}
}

// TODO: Is this global var necessary/convenient?
var universe = make(map[string]typeDecl)

func (p *parser) parsePHPDoc(doc token.Token) *phpdoc.Block {
	if doc.Type != token.DocComment {
		return nil
	}

	b, err := phpdoc.Parse(strings.NewReader(doc.Text))
	if err != nil {
		pos := doc.Pos
		if se, ok := err.(*phpdoc.SyntaxError); ok {

			if se.Line == 1 {
				pos.Column += se.Column - 1
			} else {
				pos.Line += se.Line - 1
				pos.Column = se.Column
			}

			err = se.Err

		}
		fmt.Fprintf(p.warnOut, "%s:%s: [WARN] %v\n", p.filename, pos, err)
		return nil
	}
	return b
}

func (p *parser) parseUseStmt() []UseStmt {
	use := p.parseQualifiedName()
	if p.got(token.Lbrace) {
		return p.parseGroupedUseStmt(use)
	}
	alias := use
	if i := strings.LastIndexByte(alias, '\\'); i >= 0 {
		alias = alias[i+1:]
	}
	if p.got(token.As) {
		alias = p.tok.Text
		p.expect(token.Ident)
	}
	p.expect(token.Semicolon)
	return []UseStmt{{Namespace: use, Alias: alias}}
}

func (p *parser) parseGroupedUseStmt(prefix string) []UseStmt {
	var uses []UseStmt
	for {
		part := p.parseQualifiedName()
		alias := part
		if i := strings.LastIndexByte(alias, '\\'); i >= 0 {
			alias = alias[i+1:]
		}
		if p.got(token.As) {
			alias = p.tok.Text
			p.expect(token.Ident)
		}
		uses = append(uses, UseStmt{Namespace: prefix + part, Alias: alias})
		if p.got(token.Comma) {
			continue
		}
		break
	}

	p.expect(token.Rbrace)
	return uses
}

func (p *parser) parseClass() *Class {
	p.expect(token.Class)
	name := p.tok
	switch p.tok.Type {
	case token.Enum:
		// TODO: Are any other keywords allowed?
		p.tok.Type = token.Ident
	}
	p.expect(token.Ident)
	class := name.Text
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if _, ok := universe[class]; ok {
		p.errorf("type %v already defined", class)
		return nil
	}
	c := &Class{Name: class, Properties: make(map[string]*Property), Methods: make(map[string]*Function)}
	universe[class] = c
	p.nextClass = class

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

func (p *parser) parseTrait(doc token.Token) *Trait {
	p.expect(token.Trait)
	name := p.tok
	p.expect(token.Ident)
	class := name.Text
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if _, ok := universe[class]; ok {
		p.errorf("type %v already defined", class)
		return nil
	}
	t := &Trait{Name: class, Properties: make(map[string]*Property), Methods: make(map[string]*Function)}
	universe[class] = t
	p.nextClass = class

	// TODO: Doc comment.
	return t
}

func (p *parser) parseInterface() *Class {
	p.expect(token.Interface)
	name := p.tok
	p.expect(token.Ident)
	class := name.Text
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if _, ok := universe[class]; ok {
		p.errorf("type %v already defined", class)
		return nil
	}
	i := &Class{Name: class, Methods: make(map[string]*Function)}
	universe[class] = i
	p.nextClass = class
	return i
}

func (p *parser) parseMember(doc token.Token) {
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

func (p *parser) parseFunction(doc token.Token) {
	// We don't care whether the function returns a reference, or not.
	p.consume(token.BitAnd)

	if p.tok.Type.IsKeyword() {
		p.tok.Type = token.Ident
	}
	def := p.tok
	if !p.got(token.Ident) {
		// TODO: Anonymous function not yet supported.
		return
	}
	p.parseParamList()

	var typ phptype.Type
	if p.got(token.Colon) {
		typ = p.tryParseType()
	}
	if b := p.parsePHPDoc(doc); b != nil {
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
		// TODO: Add support for regular functions.
		return
	}

	class := p.getClass(p.thisClass, typ)
	m := Function{Name: def.Text, Type: typ, Class: class}
	if err := c.addMethod(&m); err != nil {
		// TODO: Fix position of error.
		p.errorf("%v", err)
	}
}

func (p *parser) parseParamList() {
	p.params = nil
	p.expect(token.Lparen)
	for {
		switch p.tok.Type {
		case token.EOF:
			p.expect(token.Rparen)
			return
		case token.Rparen:
			p.next()
			return
		}
		if p.got(token.Hash) {
			// Attrs are ignored for now.
			// TODO: Fix that?
			p.expect(token.Lbrack)
			p.parseScope(token.Lbrack)
		}
		typ := p.tryParseType()
		name := p.tok.Text
		if !p.got(token.Var) {
			// TODO: Unsupported syntax. Giving up.
			p.next()
			continue
		}
		class := p.getClass(p.thisClass, typ)
		p.params = append(p.params, Param{Name: name, Class: class})
		if p.got(token.Assign) {
		Skip:
			// TODO: Implement proper parsing of default values.
			for {
				switch p.tok.Type {
				case token.EOF, token.Comma, token.Rparen:
					break Skip
				case token.Lparen:
					// It must be array()
					p.next()
					p.parseScope(token.Lparen)
				default:
					p.next()
				}
			}
		}
		if !p.got(token.Comma) {
			p.expect(token.Rparen)
			return
		}
	}
}

func (p *parser) parseProperty(doc token.Token) {
	typ := p.tryParseType()

	def := p.tok
	if !p.got(token.Var) {
		return
	}

	if b := p.parsePHPDoc(doc); b != nil {
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
		if strings.ContainsRune(p.thisClass, '@') {
			// TODO: Add proper support for anonymous classes.
			return
		}
		p.errorf("not in class context")
		return
	}

	name := strings.TrimPrefix(def.Text, "$")
	class := p.getClass(p.thisClass, typ)
	m := Property{Name: name, Type: typ, Class: class}
	if err := c.addProperty(&m); err != nil {
		// TODO: Fix position of error.
		p.errorf("%v", err)
	}
}

func (p *parser) parseForeach() *Foreach {
	p.expect(token.Foreach)
	p.expect(token.Lparen)

	x := p.parseExpr()
	for !p.got(token.As) {
		// Basically skip over all tokens.
		switch typ := p.tok.Type; typ {
		default:
			p.next()
			continue
		case token.Lparen, token.Lbrack:
			// TODO: Don't ignore these.
			p.next()
			p.parseScope(typ)
		}
	}

	param := p.parseForeachParam()
	if p.got(token.DoubleArrow) {
		// We only care about value, not key.
		param = p.parseForeachParam()
	}
	p.expect(token.Rparen)
	if param == nil {
		return nil
	}
	return &Foreach{
		X:     x,
		Value: *param,
	}
}

func (p *parser) parseForeachParam() *Param {
	if p.got(token.Lbrack) {
		// Giving up.
		p.parseScope(token.Lbrack)
		return nil
	}
	p.consume(token.BitAnd)
	param := Param{Name: p.tok.Text, Class: "stdClass"}
	if !p.got(token.Var) {
		return nil
	}
	if p.got(token.Lbrack) {
		// Giving up.
		p.parseScope(token.Lbrack)
		return nil
	}
	return &param
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

var anonymousCount int

func (p *parser) parseNewInstance() Expr {
	if p.got(token.Static) {
		return &NewInstance{Class: p.thisClass}
	}
	switch {
	case p.got(token.Class):
		anonymousCount++
		p.nextClass = "Anonymous@" + fmt.Sprint(anonymousCount)
		p.consume(token.Extends)
		fallthrough
	case p.got(token.Var):
		return &NewInstance{Class: "stdClass"}
	}
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
	if p.tok.Type.IsKeyword() {
		p.tok.Type = token.Ident
	}
	if p.got(token.Ident) {
		// Skip params.
		if p.got(token.Lparen) {
			a.MethodCall = true
			p.parseScope(token.Lparen)
		}
		return a
	}
	// TODO: This doesn't seem like a good default.
	return x
}

func (p *parser) getClass(thisClass string, typ phptype.Type) string {
	class := getClass(typ)
	if class == "self" {
		return thisClass
	}
	class = p.fullyQualify(class)
	return class
}
