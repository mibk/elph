package main

import (
	"fmt"
	"io"
	"os"
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

	namespace Ident
	use       map[string]Ident
	thisClass Ident
	nextClass Ident
	params    []*Param
}

func Parse(r io.Reader, filename string, php74Compat bool) (*File, error) {
	return parsePHP(r, filename, php74Compat, os.Stderr)
}

func parsePHP(r io.Reader, filename string, php74Compat bool, warnOut io.Writer) (*File, error) {
	p := &parser{scan: token.NewScanner(r, php74Compat), filename: filename, warnOut: warnOut}
	p.use = make(map[string]Ident)
	p.next() // init
	doc := p.parseFile()
	if p.err != nil {
		return nil, p.err
	}
	return doc, nil
}

const debugTypeCmd = "#debugType "

func (p *parser) next() {
	for p.tok.Type != token.EOF {
		switch p.tok = p.scan.Next(); p.tok.Type {
		default:
			return
		case token.Comment:
			// Allow this special comment.
			// Hopefully the code in the wild doesn't use it in awkward places.
			if strings.HasPrefix(p.tok.Text, debugTypeCmd) {
				return
			}
		case token.Whitespace:
		}
	}
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
	p.got(token.InlineHTML) // ignore
	if p.got(token.OpenTag) {
		file.Block = p.parseBlock(token.OpenTag)
	}
	return file
}

func (p *parser) parseBlock(open token.Type) *Block {
	s := new(Block)
	sep := token.Semicolon

	var close token.Type
	switch open {
	default:
		panic(fmt.Sprintf("unknown pair for %v", open))
	case token.OpenTag:
		close = token.EOF
	case token.Lbrace:
		s.Params = p.params
		p.params = nil
		backup := p.thisClass
		p.thisClass = p.nextClass
		defer func() { p.thisClass = backup }()

		close = token.Rbrace
	case token.Lparen:
		close = token.Rparen
	case token.Lbrack:
		close = token.Rbrack
		sep = token.Comma
	}

	for {
		stmt := p.parseStmt(sep)
		p.got(sep)
		if len(stmt.Nodes) > 0 {
			s.Stmts = append(s.Stmts, stmt)
		}

		switch typ := p.tok.Type; typ {
		case close:
			p.next()
			return s
		case token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			p.errorf("unexpected %v", typ)
			return s
		}
	}
}

func (p *parser) parseStmt(sep token.Type) (s *Stmt) {
	s = new(Stmt)
	var docComment token.Token
	afterFunc := false
	for {
		switch typ := p.tok.Type; typ {
		case sep, token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			return s
		case token.OpenTag:
			p.next()
			return s
		case token.Comment:
			pos := p.tok.Pos
			v, ok := strings.CutPrefix(p.tok.Text, debugTypeCmd)
			p.next()
			if !ok {
				panic(fmt.Sprintf("unexpected comment: %q", p.tok.Text))
			}
			s.Nodes = append(s.Nodes, &Debug{Var: strings.TrimSpace(v), Pos: pos})
		case token.DocComment:
			docComment = p.tok
			p.next()
		case token.Namespace:
			p.next()
			p.namespace = p.parseQualifiedName()
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
				b := p.parseBlock(token.Lbrace)
				s.Nodes = append(s.Nodes, b)

				if b := p.parsePHPDoc(doc); b != nil {
					for _, line := range b.Lines {
						if tag, ok := line.(*phpdoc.PropertyTag); ok {
							m := &Property{
								Name: strings.TrimPrefix(tag.Var, "$"),
								Type: p.resolveClass(c.Name, tag.Type),
							}
							c.replaceProperty(m)
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
		case token.Catch:
			c := p.parseCatch()
			s.Nodes = append(s.Nodes, c)
		case token.Lparen:
			p.next()
			b := p.parseBlock(typ)
			s.Nodes = append(s.Nodes, b)
		case token.Lbrace, token.Lbrack:
			p.next()
			b := p.parseBlock(typ)
			s.Nodes = append(s.Nodes, b)
			if typ == token.Lbrace {
				return s
			}
		case token.Var:
			e := p.parseExpr()
			s.Nodes = append(s.Nodes, e)
		case token.New:
			p.next()
			p.parseNewInstance()
		case token.Arrow, token.QmarkArrow, token.DoubleColon, token.Const:
			p.next()
			// Keywords after :: (and all the above tokens) are always idents.
			if p.tok.Type.IsKeyword() {
				p.tok.Type = token.Ident
			}
		default:
			p.next()
			docComment = token.Token{Type: token.Illegal}
		}
	}
}

// TODO: Is this global var necessary/convenient?
var universe = make(map[Ident]typeDecl)

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
	alias := string(use)
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

func (p *parser) parseGroupedUseStmt(prefix Ident) []UseStmt {
	var uses []UseStmt
	for {
		part := p.parseQualifiedName()
		alias := string(part)
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
	if p.got(token.Colon) {
		// TODO: hack
		return nil
	}
	p.expect(token.Ident)
	class := Ident(name.Text)
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if _, ok := universe[class]; ok {
		p.errorf("type %v already defined in %s", class, p.filename)
		return nil
	}
	c := &Class{Name: class}
	universe[class] = c
	p.nextClass = class

	if p.got(token.Extends) {
		e := p.parseQualifiedName()
		c.Extends = p.fullyQualify(e)
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
	class := Ident(name.Text)
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if _, ok := universe[class]; ok {
		p.errorf("type %v already defined in %s", class, p.filename)
		return nil
	}
	t := &Trait{Name: class}
	universe[class] = t
	p.nextClass = class

	// TODO: Doc comment.
	return t
}

func (p *parser) parseInterface() *Class {
	p.expect(token.Interface)
	name := p.tok
	p.expect(token.Ident)
	class := Ident(name.Text)
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if _, ok := universe[class]; ok {
		p.errorf("type %v already defined in %s", class, p.filename)
		return nil
	}
	i := &Class{Name: class}
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
	p.got(token.BitAnd) // ignore

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

	class := p.resolveClass(p.thisClass, typ)
	m := Function{Name: def.Text, Returns: class}
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
			p.parseBlock(token.Lbrack)
		}

		isMember := false
		switch p.tok.Type {
		case token.Private, token.Protected, token.Public:
			p.next()
			isMember = true
		}

		typ := p.tryParseType()
		name := p.tok.Text
		if !p.got(token.Var) {
			// TODO: Unsupported syntax. Giving up.
			p.next()
			continue
		}
		class := p.resolveClass(p.thisClass, typ)
		p.params = append(p.params, &Param{Name: name, Type: class})
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
					p.parseBlock(token.Lparen)
				default:
					p.next()
				}
			}
		}

		if isMember {
			if c, _ := universe[p.thisClass]; c != nil {
				name = strings.TrimPrefix(name, "$")
				m := Property{Name: name, Type: class}
				if err := c.addProperty(&m); err != nil {
					p.errorf("%v", err)
				}
				// TODO: Support anonymous classes.
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
		if strings.ContainsRune(string(p.thisClass), '@') {
			// TODO: Add proper support for anonymous classes.
			return
		}
		p.errorf("not in class context")
		return
	}

	name := strings.TrimPrefix(def.Text, "$")
	class := p.resolveClass(p.thisClass, typ)
	m := Property{Name: name, Type: class}
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
			p.parseBlock(typ)
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
		p.parseBlock(token.Lbrack)
		return nil
	}
	p.got(token.BitAnd) // ignore
	param := Param{Name: p.tok.Text, Type: "stdClass"}
	if !p.got(token.Var) {
		return nil
	}
	if p.got(token.Lbrack) {
		// Giving up.
		p.parseBlock(token.Lbrack)
		return nil
	}
	return &param
}

func (p *parser) parseCatch() *Param {
	p.expect(token.Catch)
	p.expect(token.Lparen)
	typ := p.tryParseType()
	class := p.resolveClass(p.thisClass, typ)
	param := Param{Type: class}
	name := p.tok.Text
	if p.got(token.Var) {
		param.Name = name
	}
	p.expect(token.Rparen)
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
		p.nextClass = Ident("Anonymous@" + fmt.Sprint(anonymousCount))
		p.got(token.Extends) // ignore
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
	if p.got(token.Lbrack) {
		// TODO: Also check index expr?
		x = &IndexExpr{X: x}
		p.parseBlock(token.Lbrack)
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
			p.parseBlock(token.Lparen)
		}
		return a
	}
	// TODO: This doesn't seem like a good default.
	return x
}

func (p *parser) resolveClass(name Ident, typ phptype.Type) Ident {
	// TODO: This method is weird. Fix it.
	class := getClass(typ)
	if class == "self" {
		return name
	}
	class = p.fullyQualify(class)
	return class
}
