package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"mibk.dev/phpfmt/token"
)

// TODO: Remove this hack.
var hasErrors = false

func Check(x any, a *Arbiter, warnOut io.Writer) {
	l := linter{
		stdout:           os.Stdout,
		stderr:           warnOut,
		arbiter:          a,
		scope:            make(map[string]Ident),
		fileBeingChecked: "<line>",
		reported:         make(map[string]bool),
	}
	l.check(x)
}

type linter struct {
	stdout  io.Writer
	stderr  io.Writer
	arbiter *Arbiter

	scope map[string]Ident

	// TODO: Fix this.
	fileBeingChecked string
	reported         map[string]bool

	thisClass *Class
	nextClass *Class
	pushScope bool
}

func (l *linter) reportf(pos token.Pos, format string, args ...any) {
	msg := fmt.Sprintf("%s:%d:%d: %s",
		l.fileBeingChecked, pos.Line, pos.Column,
		fmt.Sprintf(format, args...),
	)
	if !l.arbiter.errorMatched(msg) {
		fmt.Fprintln(l.stdout, msg)
		hasErrors = true
	}
}

func (l *linter) check(x any) {
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *File:
		l.fileBeingChecked = x.Path
		l.check(x.Block)
	case *Class:
		l.nextClass = x
		l.pushScope = true

		for _, p := range x.Properties {
			if !l.exists(p.Type) {
				l.reportf(p.Pos, "property %s has non-existing type %s", p.Name, p.Type)
				p.Type = "mixed" // Do not report the error again.
			}
			if p.DefaultValue != nil {
				l.check(p.DefaultValue)
			}
		}
		for _, m := range x.Methods {
			if !l.exists(m.Returns) {
				l.reportf(m.Pos, "method %s returns non-existing type %s", m.Name, m.Returns)
				m.Returns = "mixed" // Do not report the error again.
			}
		}

	case *Trait:
		l.nextClass = &Class{Name: "stdClass"} // Ignore
		l.pushScope = true
	case *Block:
		if l.pushScope {
			backupClass := l.thisClass
			l.thisClass = l.nextClass
			backupScope := l.scope
			l.scope = make(map[string]Ident)
			defer func() {
				l.thisClass = backupClass
				l.scope = backupScope
			}()
			l.scope["$this"] = l.thisClass.Name
			l.pushScope = false
		}
		for _, p := range x.Params {
			l.scope[p.Name] = p.Type
		}
		for _, stmt := range x.Stmts {
			l.check(stmt)
		}
	case *Foreach:
		l.check(x.X)
		v := x.Value
		l.scope[v.Name] = v.Type
	case *Param:
		l.scope[x.Name] = x.Type
		l.checkIdent(x.Pos, x.Type, "class")
		if x.DefaultValue != nil {
			l.check(x.DefaultValue)
		}
	case *Debug:
		class := l.scope[x.Var]
		if class != "" {
			l.reportf(x.Pos, "%v is of type: %v (DEBUG)", x.Var, class)
		} else {
			l.reportf(x.Pos, "unknown var: %v (DEBUG)", x.Var)
		}
	case *NewInstance:
		l.findNewInstanceType(x.Class)
		l.check(x.Class)
	case *Stmt:
		for _, n := range x.Nodes {
			l.check(n)
		}
	case *AssignExpr:
		l.check(x.Left)
		if _, checked := l.findVarType(x); !checked {
			l.check(x.Right)
		}
	case *MemberAccess:
		l.checkMemberAccess(x)
	case *IndexExpr:
		l.check(x.X)
	case *VarExpr:
	case *ValueExpr:
	case *AssertExpr:
		l.scope[x.Var] = x.Type
	}
}

func (l *linter) exists(id Ident) bool {
	id = id.unslash()
	switch {
	case isBasicType(id),
		id == "stdClass",
		strings.Contains(string(id), "<"), // TODO: Check generics <> too
		strings.Contains(string(id), "-"): // special PHPStan type
		return true
	}
	_, ok := universe[id]
	return ok
}

func (l *linter) checkIdent(pos token.Pos, id Ident, typ string) {
	if !l.exists(id) {
		l.reportf(pos, "%s %v not found", typ, id)
	}
}

func (l *linter) findVarType(a *AssignExpr) (class Ident, checked bool) {
	switch val := a.Right.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", val))
	case *NewInstance:
		class = l.findNewInstanceType(val.Class)
		checked = true
	case *ValueExpr:
		class = val.Type
	case *VarExpr:
		if strings.HasPrefix(val.Name, "$") {
			class = l.scope[val.Name]
			if class == "" {
				msg := fmt.Sprintf("unknown value of %s", val.Name)
				fmt.Fprintf(l.stderr, "%s:%s: [WARN] %v\n", l.fileBeingChecked, a.Right.Pos(), msg)
			}
		}
		// If unknown, hope for the best.
		class = cmp.Or(class, "mixed")
	case *MemberAccess:
		class = l.checkMemberAccess(val)
		checked = true
	case *AssignExpr:
		class, checked = l.findVarType(val)
	case *IndexExpr:
		// TODO: Fix this.
		class = "stdClass"
	}

	if class == "void" {
		l.reportf(a.Right.Pos(), "cannot assign '%s'", class)
		class = "stdClass"
	}

	if v, ok := a.Left.(*VarExpr); ok {
		l.scope[v.Name] = class
	}

	return class, checked
}

func (l *linter) findNewInstanceType(x any) (class Ident) {
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported expr type: %T", x))
	case *ValueExpr:
		class := x.Type.unslash()
		switch class {
		case "self", "static":
			return l.thisClass.Name
		case "stdClass":
			return x.Type
		}
		if _, ok := universe[class].(*Class); !ok {
			l.reportf(x.V, "class %v not found", class)
			return "\\stdClass"
		}
		return x.Type
	case *Class:
		return x.Name
	}
}

func (l *linter) checkMemberAccess(a *MemberAccess) Ident {
	var x Ident
	switch r := a.Rcvr.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", r))
	case *ValueExpr:
		x = r.Type
	case *VarExpr:
		// TODO: For now, let's default to mixed.
		x = cmp.Or(l.scope[r.Name], "mixed")
	case *MemberAccess:
		x = l.checkMemberAccess(r)
	case *IndexExpr:
		// TODO: Implement later.
		x = "stdClass"
	}

	if x == "self" || x == "parent" {
		// TODO: This is definitely a hack. Fix it.
		x = cmp.Or(l.thisClass, l.nextClass).Name
	} else if isBasicType(x) {
		if x == "mixed" || x == "object" {
			// All member acces allowed on mixed.
			return x
		}
		l.reportf(a.NamePos, "cannot call method on '%s'", x)
		return "<not-a-class>"
	}

	if x = x.unslash(); x == "stdClass" {
		// All member access allowed.
		return x
	}
	return l.checkClassMember(a.NamePos, x, x, a.Name, a.MethodCall, a.Static, "")
}

func (l *linter) checkClassMember(pos token.Pos, originalClass, class Ident, member string, methodCall, static bool, template Ident) Ident {
	if base, t, ok := strings.Cut(string(class), "<>"); ok {
		class, template = Ident(base), Ident(t)
	}

	// TODO: Different error if entity exists but is not a class?
	c, ok := universe[class].(*Class)
	if !ok {
		t, ok := universe[class].(*Trait)
		if !ok {
			if class == "stdClass" {
				// TODO: This hack is on too many places. Fix it.
				return class
			}
			if key := string(class) + "·" + l.fileBeingChecked; !l.reported[key] {
				l.reportf(pos, "class %v not found", class)
				if !static {
					l.reported[key] = true
				}
			}
			return "\\stdClass"
		}
		// Let's check the trait as if it were a class.
		c = &Class{
			Name:       t.Name,
			Properties: t.Properties,
			Methods:    t.Methods,
		}
	}

	for _, name := range c.Traits {
		t, ok := universe[name].(*Trait)
		if !ok {
			l.reportf(pos, "trait %v not found", name)
			continue
		}
		for _, m := range t.Properties {
			// TODO: Check whether property not already defined?
			c.addProperty(m)
		}
		for _, m := range t.Methods {
			// TODO: Check whether method not already defined?
			m := *m
			if m.Returns == t.Name {
				// TODO: This is hacky, and ugly.
				m.Returns = c.Name
			}
			c.addMethod(&m)
		}
	}
	c.Traits = nil // Mark as process.

	var memberClass Ident
	var memberType Ident

	if methodCall {
		memberType = "method"
		if m := c.Methods[member]; m != nil {
			l.checkStaticAccess(pos, member, m.Static, static, true)
			memberClass = m.Returns
		} else if p := c.Properties[member]; p != nil {
			l.checkStaticAccess(pos, member, p.Static, static, false)
			// TODO: For now, let's assume the property is a callable.
			memberClass = "mixed"
		} else if strings.HasPrefix(member, "$") {
			// We cannot decide.
			memberClass = "mixed"
		}
	} else if member, isVar := strings.CutPrefix(member, "$"); isVar && !static {
		// This is stupid dynamic $foo->$bar call. Ignore it.
		return "mixed"
	} else if !isVar && static {
		if member == "class" {
			// PHP magic constant.
			return "string"
		}
		memberType = "const"
		if c := c.Properties["#"+member]; c != nil {
			memberClass = c.Type
		}
	} else {
		memberType = "property"
		if p := c.Properties[member]; p != nil {
			l.checkStaticAccess(pos, member, p.Static, static, false)
			memberClass = p.Type
		} else {
			// TODO: Let's assume, for now,
			// that any property might be a get method.
			getter := []rune(member)
			getter[0] = unicode.ToUpper(getter[0])
			if m := c.Methods["get"+string(getter)]; m != nil && m.Static == static {
				memberClass = m.Returns
			}
		}
	}

	// Hack for generics.
	if m, ok := strings.CutSuffix(string(memberClass), "<>T"); ok {
		m := Ident(m)
		if template != "" {
			m += "<>" + template
		}
		memberClass = m
	}

	if memberClass == TemplateParam && template != "" {
		memberClass = template
	}

	if member, isVar := strings.CutPrefix(member, "$"); memberClass == "" && static && !isVar {
		// Interfaces can define constants.
		memberClass = findImplementorsConstType(c, member)
	}

	if memberClass == "" && c.Extends != "" {
		parent := c.Extends.unslash()
		if parent == "stdClass" {
			// All good.
			// TODO: Really?
			return parent
		}
		template = cmp.Or(template, c.Template)
		return l.checkClassMember(pos, originalClass, parent, member, methodCall, static, template)
	}
	if memberClass == "" {
		l.reportf(pos, "class %s %v::%v does not exist", memberType, originalClass, member)
		return "\\stdClass"
	}
	if memberClass == "static" {
		// TODO: Doesn't file like the right place for this.
		return originalClass
	}
	return memberClass
}

func findImplementorsConstType(c *Class, member string) Ident {
	for _, iface := range c.Implements {
		i, ok := universe[iface].(*Class)
		if !ok {
			// Can this happen?
			continue
		}
		if c := i.Properties["#"+member]; c != nil {
			return c.Type
		}
		if id := findImplementorsConstType(i, member); id != "" {
			return id
		}
	}
	return ""
}

func (l *linter) checkStaticAccess(pos token.Pos, memberName string, isStatic, accessStatic, methodCall bool) {
	if isStatic == accessStatic {
		return
	}

	verb, obj := "access", "property"
	if methodCall {
		verb, obj = "call", "method"
	}
	if isStatic {
		l.reportf(pos, "cannot %s static %s '%s' via object instance", verb, obj, memberName)
	} else {
		if methodCall {
			// Prevent issues with parent::foo etc.
			return
		}
		l.reportf(pos, "cannot %s instance %s '%s' statically", verb, obj, memberName)
	}
}
