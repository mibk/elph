package main

import (
	"fmt"
	"io"
	"strings"

	"mibk.dev/elph/resolved"
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

	namespace     string
	use           map[string]string
	thisClass     string
	nextClass     string
	templateParam string
	params        []*Param
	ignoreLines   map[int]string
	earlyExit     bool
}

func Parse(r io.Reader, filename string, php74Compat bool, warnOut io.Writer) (*File, error) {
	return parsePHP(r, filename, php74Compat, warnOut)
}

func parsePHP(r io.Reader, filename string, php74Compat bool, warnOut io.Writer) (*File, error) {
	p := &parser{scan: token.NewScanner(r, php74Compat), filename: filename, warnOut: warnOut}
	p.use = make(map[string]string)
	p.ignoreLines = make(map[int]string)
	p.next() // init
	doc := p.parseFile()
	if p.err != nil {
		return nil, p.err
	}
	doc.IgnoreLines = p.ignoreLines
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
			if tag, ok := strings.CutPrefix(p.tok.Text, "// @phpstan-ignore "); ok {
				tag = strings.TrimSpace(tag)
				if i := strings.IndexAny(tag, " ,"); i >= 0 {
					tag = tag[:i]
				}
				p.ignoreLines[p.tok.Pos.Line] = tag
				p.ignoreLines[p.tok.Pos.Line+1] = tag
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
		file.Block = p.parseBlock(token.OpenTag, false)
	}
	return file
}

func (p *parser) parseBlock(open token.Type, classRoot bool) *Block {
	b := new(Block)
	sep := token.Semicolon

	var close token.Type
	switch open {
	default:
		panic(fmt.Sprintf("unknown pair for %v", open))
	case token.OpenTag:
		close = token.EOF
	case token.Lbrace:
		b.Params = p.params
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
		stmt := p.parseStmt(sep, classRoot)
		p.got(sep)
		if len(stmt.Nodes) > 0 {
			b.Stmts = append(b.Stmts, stmt)
		}

		switch typ := p.tok.Type; typ {
		case close:
			p.next()
			return b
		case token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
			p.errorf("unexpected %v", typ)
			return b
		}
	}
}

func (p *parser) parseStmt(sep token.Type, classRoot bool) (s *Stmt) {
	s = new(Stmt)
	p.earlyExit = false
	var docComment token.Token
	afterFunc := false
	for {
		switch typ := p.tok.Type; typ {
		case sep, token.Semicolon, token.EOF, token.Rparen, token.Rbrace, token.Rbrack:
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
			if p.got(token.Hash) {
				// Ignore any attributes.
				p.expect(token.Lbrack)
				p.parseBlock(token.Lbrack, false)
			}
		case token.Namespace:
			p.next()
			p.namespace = p.parseQualifiedName()
		case token.Use:
			p.next()
			if afterFunc || p.got(token.Function) || p.got(token.Const) {
				for p.tok.Type == token.Ident {
					// Just ignore it.
					p.parseQualifiedName()
					if p.got(token.Comma) {
						continue
					}
					break
				}
				continue
			}
			for _, use := range p.parseUseStmt() {
				p.use[use.Alias] = use.Namespace
			}
		case token.Abstract, token.Final:
			p.next()
		case token.Class:
			doc := docComment
			if c := p.parseClass(); c != nil {
				s.Nodes = append(s.Nodes, c)
				if b := p.parsePHPDoc(doc); b != nil {
					p.extractTemplateParam(c, b)
				}
				backupTP := p.templateParam
				p.templateParam = c.TemplateParam
				b := p.parseBlock(token.Lbrace, true)
				s.Nodes = append(s.Nodes, b)

				if b := p.parsePHPDoc(doc); b != nil {
					p.handleClassDoc(c, b, doc.Pos)
				}
				p.templateParam = backupTP
			}
		case token.Trait:
			if c := p.parseTrait(docComment); c != nil {
				s.Nodes = append(s.Nodes, c)
			}
		case token.Interface:
			doc := docComment
			if c := p.parseInterface(); c != nil {
				s.Nodes = append(s.Nodes, c)
				if b := p.parsePHPDoc(doc); b != nil {
					p.extractTemplateParam(c, b)
				}
				backupTP := p.templateParam
				p.templateParam = c.TemplateParam
				b := p.parseBlock(token.Lbrace, true)
				s.Nodes = append(s.Nodes, b)
				p.templateParam = backupTP
			}
		case token.Enum:
			if p.tok.Text != "enum" {
				// This is a hack to support e.g. constants named Enum etc.
				p.next()
				break
			}
			if c := p.parseEnum(); c != nil {
				s.Nodes = append(s.Nodes, c)
				b := p.parseBlock(token.Lbrace, true)
				s.Nodes = append(s.Nodes, b)
			}
		case token.Static:
			p.next()
			if classRoot {
				p.parseMember(docComment, true)
			}
		case token.Private, token.Protected, token.Public:
			p.next()
			p.parseMember(docComment, false)
		case token.Const:
			p.next()
			p.parseProperty(docComment, false, true)
		case token.Function:
			p.next()
			p.parseFunction(docComment, false)
			// Disarm parsing 'use' stmt after 'function' for now.
			afterFunc = true
		case token.Case:
			p.next()
			if classRoot {
				p.parseProperty(docComment, false, true)
			}
		case token.Foreach:
			if f := p.parseForeach(); f != nil {
				s.Nodes = append(s.Nodes, f)
			}
		case token.Catch:
			c := p.parseCatch()
			s.Nodes = append(s.Nodes, c)
		case token.Lparen:
			p.next()
			b := p.parseBlock(typ, false)
			s.Nodes = append(s.Nodes, b)
		case token.Lbrace:
			p.next()
			b := p.parseBlock(typ, false)
			s.Nodes = append(s.Nodes, b)
			return s
		case token.Lbrack:
			p.next()
			b := p.parseBlock(typ, false)
			s.Nodes = append(s.Nodes, b)
		case token.Fn:
			p.next()
			p.parseParamList()
			if p.got(token.Colon) {
				p.tryParseType()
			}
			p.got(token.Arrow)
			b := &Block{Params: p.params}
			p.params = nil
			body := p.parseStmt(sep, classRoot)
			b.Stmts = append(b.Stmts, body)
			s.Nodes = append(s.Nodes, b)
			continue
		case token.Backslash, token.Ident:
			if a := p.tryParseStaticMemberAccess(); a != nil {
				s.Nodes = append(s.Nodes, a)
			}
		case token.Var:
			e := p.parseExpr()
			s.Nodes = append(s.Nodes, e)
		case token.New:
			p.next()
			e := p.parseNewInstance()
			s.Nodes = append(s.Nodes, e)
		case token.If:
			p.tryParseInstanceofGuard(s)
		case token.Arrow, token.QmarkArrow, token.DoubleColon, token.Instanceof:
			p.next()
		case token.Continue, token.Return, token.Break, token.Throw:
			p.earlyExit = true
			p.next()
		default:
			p.next()
			docComment = token.Token{Type: token.Illegal}
		}
	}
}

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
	if p.tok.Type.IsReserved() {
		p.tok.Type = token.Ident
	}
	if p.got(token.Colon) {
		// Bail out: "class" used as a named argument, e.g. foo(class: ...).
		return nil
	}
	p.expect(token.Ident)
	class := name.Text
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if e, ok := universe[class]; ok {
		p.errorf("type %v already defined in %s", class, e.sourceFile())
		return nil
	}
	c := &Class{Name: class, SourceFile: p.filename}
	universe[class] = c
	p.nextClass = class

	if p.got(token.Extends) {
		e := p.parseQualifiedName()
		c.Extends = p.fullyQualify(e)
	}
	if p.got(token.Implements) {
		for {
			i := p.parseQualifiedName()
			c.Implements = append(c.Implements, p.fullyQualify(i))
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
		if p.got(token.Lbrace) {
			// TODO: Add support for trait config.
			p.parseBlock(token.Lbrace, false)
		} else {
			p.expect(token.Semicolon)
		}
	}

	return c
}

func (p *parser) extractTemplateParam(c *Class, b *phpdoc.Block) {
	for _, line := range b.Lines {
		if tag, ok := line.(*phpdoc.TemplateTag); ok {
			c.TemplateParam = tag.Param
			if tag.Bound != nil {
				c.TemplateBound = p.resolveClass(c.Name, tag.Bound)
			}
		}
	}
}

func (p *parser) handleClassDoc(c *Class, b *phpdoc.Block, pos token.Pos) {
	for _, line := range b.Lines {
		switch tag := line.(type) {
		case *phpdoc.TemplateTag:
			c.TemplateParam = tag.Param
			if tag.Bound != nil {
				c.TemplateBound = p.resolveClass(c.Name, tag.Bound)
			}
		case *phpdoc.TypeDefTag:
			adhocType := p.fullyQualify(tag.Name)
			universe[adhocType] = &Class{Name: adhocType, Extends: "stdClass", SourceFile: c.SourceFile}
		case *phpdoc.OtherTag:
			if tag.Name == "phpstan-import-type" {
				p.handleImportedPHPStanType(tag.Desc)
			}
		case *phpdoc.PropertyTag:
			m := &Property{
				Pos:  pos,
				Name: strings.TrimPrefix(tag.Var, "$"),
				Type: p.resolveType(c.Name, tag.Type),
			}
			c.replaceProperty(m)
		case *phpdoc.MethodTag:
			m := &Function{
				Pos:     pos,
				Name:    tag.Name,
				Returns: p.resolveType(c.Name, tag.Result),
				Static:  tag.Static,
			}
			c.replaceMethod(m)
		case *phpdoc.ExtendsTag:
			if g, ok := tag.Class.(*phptype.Generic); ok && len(g.TypeParams) == 1 {
				c.Template = p.resolveClass(p.thisClass, g.TypeParams[0])
			}
		}
	}
}

func (p *parser) handleImportedPHPStanType(s string) {
	s, alias, _ := strings.Cut(s, " as ")
	s = strings.TrimSpace(s)
	alias = strings.TrimSpace(alias)

	typ, class, _ := strings.Cut(s, " from ")
	typ = strings.TrimSpace(typ)
	class = strings.TrimSpace(class)

	// TODO: There's many edge cases that I ignored.
	if i := strings.LastIndex(class, `\`); i >= 0 {
		importedType := p.fullyQualify(class[:i] + `\` + typ)

		// TODO: Probably wrong source file.
		c := &Class{Name: p.fullyQualify(alias), Extends: importedType, SourceFile: p.filename}
		universe[c.Name] = c
	}
}

func (p *parser) parseTrait(_ token.Token) *Trait {
	p.expect(token.Trait)
	name := p.tok
	p.expect(token.Ident)
	class := name.Text
	if p.namespace != "" {
		class = p.namespace + `\` + class
	}

	if e, ok := universe[class]; ok {
		p.errorf("type %v already defined in %s", class, e.sourceFile())
		return nil
	}
	t := &Trait{Name: class, SourceFile: p.filename}
	universe[class] = t
	p.nextClass = class

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

	if e, ok := universe[class]; ok {
		p.errorf("type %v already defined in %s", class, e.sourceFile())
		return nil
	}
	i := &Class{Name: class, SourceFile: p.filename}
	universe[class] = i
	p.nextClass = class

	if p.got(token.Extends) {
		for {
			id := p.parseQualifiedName()
			i.Implements = append(i.Implements, p.fullyQualify(id))
			if !p.got(token.Comma) {
				break
			}
		}
	}

	p.expect(token.Lbrace)
	return i
}

func (p *parser) parseEnum() *Class {
	p.expect(token.Enum)
	name := p.tok
	p.expect(token.Ident)
	enum := name.Text
	if p.namespace != "" {
		enum = p.namespace + `\` + enum
	}

	if e, ok := universe[enum]; ok {
		p.errorf("type %v already defined in %s", enum, e.sourceFile())
		return nil
	}

	if p.got(token.Colon) {
		p.expect(token.Ident)
	}
	p.expect(token.Lbrace)

	e := &Class{Name: enum, SourceFile: p.filename}
	m := Function{Name: "tryFrom", Returns: &resolved.Basic{Name: "self"}, Static: true}
	e.addMethod(&m)
	m = Function{Name: "from", Returns: &resolved.Basic{Name: "self"}, Static: true}
	e.addMethod(&m)
	universe[enum] = e
	p.nextClass = enum

	for p.got(token.Use) {
		use := p.parseQualifiedName()
		use = p.fullyQualify(use)
		e.Traits = append(e.Traits, use)
		if p.got(token.Lbrace) {
			// TODO: Add support for trait config.
			p.parseBlock(token.Lbrace, false)
		} else {
			p.expect(token.Semicolon)
		}
	}

	return e
}

func (p *parser) parseMember(doc token.Token, static bool) {
	if !static {
		static = p.got(token.Static)
	}

	if p.got(token.Const) {
		p.parseProperty(doc, static, true)
	} else if p.got(token.Function) {
		p.parseFunction(doc, static)
	} else {
		p.parseProperty(doc, static, false)
	}
}

func (p *parser) parseFunction(doc token.Token, static bool) {
	// We don't care whether the function returns a reference, or not.
	p.got(token.BitAnd) // ignore

	name := p.tok.Text
	pos := p.tok.Pos
	if !p.got(token.Ident) {
		// TODO: Prevent adding it as a method to p.thisClass.
		name = "anonymousFn@" + fmt.Sprint(anonymousCount)
		anonymousCount++
	}
	p.parseParamList()

	var typ phptype.Type
	if p.got(token.Colon) {
		typ = p.tryParseType()
	}
	if b := p.parsePHPDoc(doc); b != nil {
	Loop:
		for _, line := range b.Lines {
			switch tag := line.(type) {
			case *phpdoc.TemplateTag:
				// If there's a template param, just give up.
				break Loop
			case *phpdoc.ParamTag:
				p.replaceParam(tag.Param)
			case *phpdoc.ReturnTag:
				typ = tag.Type
				break Loop
			}
		}
	}

	if typ == nil {
		typ = &phptype.Named{Parts: []string{"mixed"}}
	}

	c := universe[p.thisClass]
	if c == nil {
		// TODO: Add support for regular functions.
		return
	}

	classTyp := p.resolveType(p.thisClass, typ)
	m := Function{Pos: pos, Name: name, Returns: classTyp, Static: static}
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
			p.expect(token.Lbrack)
			p.parseBlock(token.Lbrack, false)
		}

		isMember := false
		switch p.tok.Type {
		case token.Private, token.Protected, token.Public:
			p.next()
			isMember = true
		}

		typ := p.tryParseType()
		name := p.tok.Text
		pos := p.tok.Pos
		if !p.got(token.Var) {
			// Unsupported syntax. Giving up.
			p.next()
			continue
		}
		classTyp := p.resolveType(p.thisClass, typ)
		par := &Param{Pos: pos, Name: name, Type: classTyp}
		p.params = append(p.params, par)
		if p.got(token.Assign) {
			def := p.parseStmt(token.Comma, false)
			par.DefaultValue = def
		}

		if isMember {
			if c := universe[p.thisClass]; c != nil {
				name = strings.TrimPrefix(name, "$")
				m := Property{Pos: pos, Name: name, Type: classTyp}
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

func (p *parser) replaceParam(u *phptype.Param) {
	for _, param := range p.params {
		if param.Name == "$"+u.Name {
			param.Type = p.resolveType(p.thisClass, u.Type)
		}
	}
}

func (p *parser) parseProperty(doc token.Token, static, constant bool) {
	p.got(token.Readonly) // ignore

	if p.tok.Type.IsReserved() {
		p.tok.Type = token.Ident
	}

	var def token.Token
	var typ phptype.Type
	if constant {
		saved := p.tok
		typ = p.tryParseType()
		if typ != nil && p.tok.Type == token.Ident {
			// Typed constant (e.g., const int FOO).
			def = p.tok
			if !p.got(token.Ident) {
				return
			}
		} else {
			// Untyped constant; tryParseType consumed the name.
			def = saved
			typ = nil
		}
	} else {
		typ = p.tryParseType()
		def = p.tok
		if !p.got(token.Var) {
			return
		}
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
		typ = &phptype.Named{Parts: []string{"mixed"}}
	}

	c := universe[p.thisClass]
	if c == nil {
		if strings.ContainsRune(p.thisClass, '@') {
			// TODO: Add proper support for anonymous classes.
			return
		}
		p.errorf("not in class context")
		return
	}

	for {
		name := strings.TrimPrefix(def.Text, "$")
		classTyp := p.resolveType(p.thisClass, typ)
		m := Property{Pos: def.Pos, Name: name, Type: classTyp, Static: static}
		if constant {
			if err := c.addConstant(&m); err != nil {
				// TODO: Fix position of error.
				p.errorf("%v", err)
			}
		} else {
			if err := c.addProperty(&m); err != nil {
				// TODO: Fix position of error.
				p.errorf("%v", err)
			}
		}
		m.DefaultValue = p.parseStmt(token.Comma, false)
		if p.got(token.EOF) || p.got(token.Semicolon) {
			return
		}
		if p.got(token.Comma) {
			def = p.tok
			if !p.got(token.Var) && !p.got(token.Ident) {
				return
			}
			continue
		}
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
			p.parseBlock(typ, false)
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
		p.parseBlock(token.Lbrack, false)
		return nil
	}
	p.got(token.BitAnd) // ignore
	param := Param{Pos: p.tok.Pos, Name: p.tok.Text, Type: &resolved.Basic{Name: "mixed"}}
	if !p.got(token.Var) {
		return nil
	}
	for p.got(token.Arrow) {
		// Ignore this exotic foreach.
		p.expect(token.Ident)
	}
	if p.got(token.Lbrack) {
		// Giving up.
		p.parseBlock(token.Lbrack, false)
		return nil
	}
	return &param
}

func (p *parser) parseCatch() *Param {
	p.expect(token.Catch)
	p.expect(token.Lparen)
	pos := p.tok.Pos
	typ := p.tryParseType()
	classTyp := p.resolveType(p.thisClass, typ)
	param := Param{Pos: pos, Type: classTyp}
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
		switch {
		case p.got(token.New):
			v = p.parseNewInstance()
		case p.tok.Type == token.Backslash || p.tok.Type == token.Ident:
			v = p.tryParseStaticMemberAccess()
			if v != nil {
				break
			}
			fallthrough
		default:
			v = p.parseExpr()
		}
		if isBinaryOp(p.tok.Type) {
			v = &ValueExpr{V: v.Pos(), Type: &resolved.Basic{Name: "mixed"}}
		}
		e = &AssignExpr{e, v}
	}
	return e
}

func isBinaryOp(t token.Type) bool {
	switch t {
	case token.Eq, token.Neq, token.Identical, token.NotIdentical,
		token.Lt, token.Gt, token.Leq, token.Geq, token.Spaceship,
		token.And, token.Or,
		token.Add, token.Sub, token.Mul, token.Quo, token.Rem, token.Pow,
		token.BitAnd, token.BitOr, token.BitXor, token.BitShl, token.BitShr,
		token.Concat, token.Coalesce, token.Qmark:
		return true
	}
	return false
}

var anonymousCount int

func (p *parser) parseNewInstance() Expr {
	pos := p.tok.Pos
	if p.got(token.Static) {
		return &NewInstance{Class: &ValueExpr{V: pos, Type: toType(p.thisClass)}}
	}
	switch class := "mixed"; {
	case p.got(token.Class):
		anonymousCount++
		class = "AnonymousClass@" + fmt.Sprint(anonymousCount)
		c := &Class{Name: class, SourceFile: p.filename}
		universe[class] = c
		p.nextClass = class

		if p.got(token.Lparen) {
			p.parseBlock(token.Lparen, false) // ignore args
		}

		if p.got(token.Extends) {
			e := p.parseQualifiedName()
			c.Extends = p.fullyQualify(e)
		}
		// TODO: implements
		return &NewInstance{Class: c}
	case p.got(token.Var):
		// Just give up; we can't know the type.
		return &NewInstance{Class: &ValueExpr{V: pos, Type: &resolved.Basic{Name: "mixed"}}}
	default:
		name := p.parseQualifiedName()
		if name == "" {
			p.expect(token.Ident)
			return nil
		}
		name = p.fullyQualify(name)
		return &NewInstance{Class: &ValueExpr{V: pos, Type: toType(name)}}
	}
}

func (p *parser) parseVarExpr() Expr {
	x := &VarExpr{Dollar: p.tok.Pos, Name: p.tok.Text}
	if !p.got(token.Var) {
		x.Name = "<unknown-expr>"
		return x
	}
	return p.parseChainAccess(x)
}

func (p *parser) parseChainAccess(x Expr) Expr {
	for {
		static := false
		switch {
		case p.got(token.Lbrack):
			// TODO: Also check index expr?
			x = &IndexExpr{X: x}
			p.parseBlock(token.Lbrack, false)
		case p.got(token.DoubleColon):
			static = true
			fallthrough
		case p.got(token.Arrow), p.got(token.QmarkArrow):
			x = p.parseMemberAccess(x, static)
		case p.got(token.Lparen):
			// TODO: This is a callback call. Support it?
			p.parseBlock(token.Lparen, false)
			x = &ValueExpr{V: x.Pos(), Type: &resolved.Basic{Name: "mixed"}}
		default:
			return x
		}
	}
}

func (p *parser) parseMemberAccess(x Expr, static bool) Expr {
	a := &MemberAccess{Rcvr: x, NamePos: p.tok.Pos, Name: p.tok.Text, Static: static}

	if !p.got(token.Ident) && !p.got(token.Var) {
		// TODO: This doesn't seem like a good default.
		return x
	}

	// Skip params.
	if p.got(token.Lparen) {
		if p.got(token.Ellipsis) && p.got(token.Rparen) {
			// TODO: Return concrete callback type?
			return &ValueExpr{V: x.Pos(), Type: &resolved.Basic{Name: "callable"}}
		}
		a.MethodCall = true
		a.Args = p.parseBlock(token.Lparen, false)
	}
	return a
}

func (p *parser) tryParseStaticMemberAccess() Expr {
	x := &ValueExpr{V: p.tok.Pos}
	id := p.parseQualifiedName()

	if id == "assert" {
		return p.parseAssert(p.tok.Pos)
	}
	if id == "unset" {
		return p.parseUnset(x.V)
	}

	x.Type = toType(p.fullyQualify(id))
	if p.got(token.DoubleColon) {
		x := p.parseMemberAccess(x, true)
		return p.parseChainAccess(x)
	}
	return nil
}

func (p *parser) parseAssert(pos token.Pos) (x Expr) {
	defer func() {
		if x == nil {
			x = &ValueExpr{V: p.tok.Pos, Type: &resolved.Basic{Name: "mixed"}}
			p.parseBlock(token.Lparen, false)
		}
	}()

	p.expect(token.Lparen)
	v := p.tok
	if !p.got(token.Var) {
		return nil
	}
	varName := v.Text
	if p.got(token.Arrow) {
		prop := p.tok
		if !p.got(token.Ident) {
			return nil
		}
		varName = varName + "->" + prop.Text
	}
	if !p.got(token.Instanceof) {
		return nil
	}
	id := p.parseQualifiedName()
	if !p.got(token.Rparen) {
		return nil
	}
	id = p.fullyQualify(id)
	return &AssertExpr{Fn: pos, Var: varName, Type: toType(id)}
}

func (p *parser) parseUnset(pos token.Pos) Expr {
	p.expect(token.Lparen)
	u := &UnsetExpr{Fn: pos}
	for {
		v := p.tok
		if !p.got(token.Var) {
			p.parseBlock(token.Lparen, false)
			return u
		}
		if p.tok.Type == token.Comma || p.tok.Type == token.Rparen {
			u.Vars = append(u.Vars, v.Text)
		} else {
			// Compound target (e.g. $arr[$k], $obj->prop,
			// $a[$i][$j]) — skip it; the variable itself
			// stays in scope.
			p.parseBlock(token.Lparen, false)
			return u
		}
		if !p.got(token.Comma) {
			break
		}
	}
	p.expect(token.Rparen)
	return u
}

// tryParseInstanceofGuard tries to match one of:
//
//	if (!$var instanceof Type) { ... }   → narrows after the if-block
//	if ($var instanceof Type) { ... }    → narrows inside the if-body
//
// and appends the appropriate nodes to s.
func (p *parser) tryParseInstanceofGuard(s *Stmt) {
	p.next() // consume 'if'
	if !p.got(token.Lparen) {
		return
	}

	negated := p.got(token.Not)

	v := p.tok
	if !p.got(token.Var) {
		p.parseBlock(token.Lparen, false)
		return
	}
	varName := v.Text
	if p.got(token.Arrow) {
		prop := p.tok
		if !p.got(token.Ident) {
			p.parseBlock(token.Lparen, false)
			return
		}
		varName = varName + "->" + prop.Text
	}
	if !p.got(token.Instanceof) {
		p.parseBlock(token.Lparen, false)
		return
	}
	if p.tok.Type != token.Ident && p.tok.Type != token.Backslash {
		p.parseBlock(token.Lparen, false)
		return
	}
	id := p.parseQualifiedName()
	if id == "" || !p.got(token.Rparen) {
		p.parseBlock(token.Lparen, false)
		return
	}
	id = p.fullyQualify(id)
	assert := &AssertExpr{Fn: v.Pos, Var: varName, Type: toType(id)}

	if negated {
		// if (!$var instanceof Type) { ... } — narrow after the block.
		if p.got(token.Lbrace) {
			p.parseBlock(token.Lbrace, false)
		}
		p.skipElseChain()
		s.Nodes = append(s.Nodes, assert)
		return
	}

	// if ($var instanceof Type) { ... } — narrow inside the body.
	if p.got(token.Lbrace) {
		b := p.parseBlock(token.Lbrace, false)
		s.Nodes = append(s.Nodes, &NarrowBlock{
			Var:       assert.Var,
			Type:      assert.Type,
			Block:     b,
			EarlyExit: p.earlyExit,
		})
	}
	p.skipElseChain()
}

func (p *parser) skipElseChain() {
	for p.got(token.Else) {
		if p.got(token.If) {
			if p.got(token.Lparen) {
				p.parseBlock(token.Lparen, false)
			}
		}
		if p.got(token.Lbrace) {
			p.parseBlock(token.Lbrace, false)
		}
	}
}

func (p *parser) resolveClass(thisClass string, typ phptype.Type) string {
	var name string
	switch typ := typ.(type) {
	case *phptype.Named:
		name = strings.Join(typ.Parts, `\`)
		if typ.Global {
			name = `\` + name
		}
	case *phptype.This:
		name = "static"
	default:
		name = "mixed"
	}
	if name == "self" {
		return thisClass
	}
	if c, ok := universe[thisClass].(*Class); ok && c.TemplateParam != "" && name == c.TemplateParam {
		return name
	}
	return p.fullyQualify(name)
}

// resolveType converts a phptype.Type to a resolved.Type,
// resolving names via the parser's namespace/use context.
func (p *parser) resolveType(thisClass string, typ phptype.Type) resolved.Type {
	switch typ := typ.(type) {
	case nil:
		return &resolved.Basic{Name: "mixed"}
	case *phptype.Union:
		var types []resolved.Type
		for _, t := range typ.Types {
			rt := p.resolveType(thisClass, t)
			// If any member is stdClass or mixed, collapse to that.
			if n, ok := rt.(*resolved.Named); ok && n.Name == "stdClass" {
				return rt
			}
			if b, ok := rt.(*resolved.Basic); ok {
				if b.Name == "mixed" {
					return rt
				}
				if strings.ToLower(b.Name) == "null" {
					continue
				}
			}
			types = append(types, rt)
		}
		if len(types) == 0 {
			return &resolved.Basic{Name: "mixed"}
		}
		return resolved.NewUnion(types...)
	case *phptype.Array:
		return &resolved.Array{Elem: p.resolveType(thisClass, typ.Elem)}
	case *phptype.Generic:
		base := p.resolveType(thisClass, typ.Base)
		if len(typ.TypeParams) == 0 {
			return base
		}
		return &resolved.Generic{
			Base:  base,
			Param: p.resolveType(thisClass, typ.TypeParams[0]),
		}
	case *phptype.Nullable:
		return p.resolveType(thisClass, typ.Type)
	case *phptype.Named:
		name := strings.Join(typ.Parts, `\`)
		if typ.Global {
			name = `\` + name
		}
		if strings.ToLower(name) == "null" {
			return &resolved.Basic{Name: "null"}
		}
		if name == "self" {
			return toType(thisClass)
		}
		if c, ok := universe[thisClass].(*Class); ok && c.TemplateParam != "" && name == c.TemplateParam {
			return &resolved.Named{Name: name}
		}
		name = p.fullyQualify(name)
		if resolved.IsBasicName(name) {
			return &resolved.Basic{Name: name}
		}
		return &resolved.Named{Name: name}
	case *phptype.ArrayShape, *phptype.ObjectShape:
		return &resolved.Named{Name: "stdClass"}
	case *phptype.This:
		return &resolved.Basic{Name: "static"}
	case *phptype.Conditional:
		return p.resolveType(thisClass, typ.True)
	default:
		return &resolved.Basic{Name: "mixed"}
	}
}
