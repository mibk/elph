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

// hasErrors is set to true when any error is reported.
var hasErrors = false

func Check(file *File, a *Arbiter, warnOut io.Writer) {
	l := linter{
		stdout:           os.Stdout,
		stderr:           warnOut,
		arbiter:          a,
		scope:            make(map[string]Ident),
		fileBeingChecked: file.Path,
		reported:         make(map[string]bool),
		ignoreLines:      file.IgnoreLines,
	}
	l.check(file.Block)
}

type linter struct {
	stdout  io.Writer
	stderr  io.Writer
	arbiter *Arbiter

	scope map[string]Ident

	fileBeingChecked string
	reported         map[string]bool
	ignoreLines      map[int]string

	thisClass *Class
	nextClass *Class
	pushScope bool
}

var ignoreTagPatterns = map[string]string{
	"property.notFound": "class property ",
	"method.notFound":   "class method ",
}

func (l *linter) reportf(pos token.Pos, format string, args ...any) {
	detail := fmt.Sprintf(format, args...)
	if tag := l.ignoreLines[pos.Line]; tag != "" {
		if prefix, ok := ignoreTagPatterns[tag]; ok && strings.HasPrefix(detail, prefix) {
			return
		}
	}
	msg := fmt.Sprintf("%s:%d:%d: %s",
		l.fileBeingChecked, pos.Line, pos.Column,
		detail,
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
	case *Class:
		backup := l.thisClass
		l.thisClass = x
		tparam := Ident(x.TemplateParam)
		for _, p := range x.Properties {
			if tparam == "" || p.Type != tparam {
				if !l.exists(p.Type) {
					l.reportf(p.Pos, "property %s has non-existing type %s", p.Name, p.Type)
					p.Type = "mixed" // Do not report the error again.
				}
			}
			if p.DefaultValue != nil {
				l.check(p.DefaultValue)
			}
		}
		for _, p := range x.Constants {
			if tparam == "" || p.Type != tparam {
				if !l.exists(p.Type) {
					l.reportf(p.Pos, "constant %s has non-existing type %s", p.Name, p.Type)
					p.Type = "mixed" // Do not report the error again.
				}
			}
			if p.DefaultValue != nil {
				l.check(p.DefaultValue)
			}
		}
		for _, m := range x.Methods {
			if tparam == "" || m.Returns != tparam {
				if !l.exists(m.Returns) {
					l.reportf(m.Pos, "method %s returns non-existing type %s", m.Name, m.Returns)
					m.Returns = "mixed" // Do not report the error again.
				}
			}
		}

		l.thisClass = backup
		l.nextClass = x
		l.pushScope = true
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
		typ := l.resolveExprType(x.X)
		v := x.Value
		if elem, ok := arrayElemType(typ); ok {
			l.scope[v.Name] = elem
		} else {
			l.scope[v.Name] = v.Type // fallback to "mixed"
		}
	case *Param:
		l.scope[x.Name] = x.Type
		if l.thisClass == nil || l.thisClass.TemplateParam == "" || x.Type != Ident(l.thisClass.TemplateParam) {
			l.checkIdent(x.Pos, x.Type, "class")
		}
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
		if x.Args != nil {
			l.check(x.Args)
		}
	case *IndexExpr:
		l.check(x.X)
	case *VarExpr:
	case *ValueExpr:
	case *AssertExpr:
		l.scope[x.Var] = x.Type
	case *UnsetExpr:
		for _, v := range x.Vars {
			delete(l.scope, v)
		}
	case *NarrowBlock:
		backup := l.scope[x.Var]
		l.scope[x.Var] = x.Type
		l.check(x.Block)
		if x.EarlyExit {
			l.scope[x.Var] = subtractType(backup, x.Type)
		} else {
			l.scope[x.Var] = backup
		}
	}
}

func subtractType(union, excluded Ident) Ident {
	parts := strings.Split(string(union), "|")
	var remaining []string
	for _, p := range parts {
		if p != string(excluded) {
			remaining = append(remaining, p)
		}
	}
	if len(remaining) == 0 {
		return "mixed"
	}
	return Ident(strings.Join(remaining, "|"))
}

func (l *linter) exists(id Ident) bool {
	if strings.Contains(string(id), "|") {
		for _, part := range strings.Split(string(id), "|") {
			if !l.exists(Ident(part)) {
				return false
			}
		}
		return true
	}
	switch {
	case isBasicType(id),
		id == "stdClass",
		strings.Contains(string(id), "<"), // TODO: Check generics <> too
		strings.Contains(string(id), "-"): // special PHPStan type
		return true
	}
	if elem, ok := arrayElemType(id); ok {
		return l.exists(elem)
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
		class = "mixed"
	}

	if class == "void" {
		l.reportf(a.Right.Pos(), "cannot assign '%s'", class)
		class = "mixed"
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
		class := x.Type
		switch class {
		case "self", "static":
			if l.thisClass == nil {
				l.reportf(x.V, "not in class context")
				return "mixed"
			}
			return l.thisClass.Name
		case "stdClass", "mixed":
			return x.Type
		}
		if _, ok := universe[class].(*Class); !ok {
			l.reportf(x.V, "class %v not found", class)
			return "mixed"
		}
		return x.Type
	case *Class:
		return x.Name
	}
}

func (l *linter) resolveExprType(x any) Ident {
	switch x := x.(type) {
	case *VarExpr:
		if t := l.scope[x.Name]; t != "" {
			return t
		}
	case *MemberAccess:
		return l.checkMemberAccess(x)
	}
	l.check(x)
	return "mixed"
}

func (l *linter) checkMemberAccess(a *MemberAccess) Ident {
	var x Ident
	switch r := a.Rcvr.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", r))
	case *ValueExpr:
		x = r.Type
	case *VarExpr:
		if override, ok := l.scope[r.Name+"->"+a.Name]; ok {
			return override
		}
		// TODO: For now, let's default to mixed.
		x = cmp.Or(l.scope[r.Name], "mixed")
	case *MemberAccess:
		x = l.checkMemberAccess(r)
	case *IndexExpr:
		if t := l.resolveExprType(r.X); t != "" {
			if elem, ok := arrayElemType(t); ok {
				x = elem
				break
			}
		}
		x = "mixed"
	}

	if strings.Contains(string(x), "|") {
		for _, part := range strings.Split(string(x), "|") {
			p := Ident(part)
			if isBasicType(p) {
				continue
			}
			if _, ok := arrayElemType(p); ok {
				continue
			}
			result := l.checkClassMember(a.NamePos, p, p, a.Name, a.MethodCall, a.Static, "")
			if result != "mixed" {
				return result
			}
		}
		return "mixed"
	}

	if l.thisClass != nil && l.thisClass.TemplateParam != "" && x == Ident(l.thisClass.TemplateParam) {
		if l.thisClass.TemplateBound != "" {
			x = l.thisClass.TemplateBound
		} else {
			return "mixed"
		}
	}

	if x == "self" || x == "parent" {
		// TODO: This is definitely a hack. Fix it.
		x = cmp.Or(l.thisClass, l.nextClass).Name
	} else if isBasicType(x) {
		if x == "mixed" || x == "object" {
			// All member access allowed on mixed.
			return x
		}
		l.reportf(a.NamePos, "cannot call method on '%s'", x)
		return "<not-a-class>"
	}

	if x == "stdClass" {
		// All member access allowed.
		return x
	}
	if _, ok := arrayElemType(x); ok {
		// Member access on an array is allowed (e.g., $arr->count()).
		return "mixed"
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
			return "mixed"
		}
		// Let's check the trait as if it were a class.
		c = &Class{
			Name:       t.Name,
			Properties: t.Properties,
			Constants:  t.Constants,
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
		for _, m := range t.Constants {
			c.addConstant(m)
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
	c.Traits = nil // Mark as processed.

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
		if c := c.Constants[member]; c != nil {
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
	if c.TemplateParam != "" {
		if m, ok := strings.CutSuffix(string(memberClass), "<>"+c.TemplateParam); ok {
			m := Ident(m)
			if template != "" {
				m += "<>" + template
			}
			memberClass = m
		}
	}

	if c.TemplateParam != "" && memberClass == Ident(c.TemplateParam) && template != "" {
		memberClass = template
	}

	if member, isVar := strings.CutPrefix(member, "$"); memberClass == "" && static && !isVar {
		// Interfaces can define constants.
		memberClass = findImplementorsConstType(c, member)
	}

	if memberClass == "" && c.Extends != "" {
		parent := c.Extends
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
		return "mixed"
	}
	if memberClass == "static" {
		// TODO: Doesn't feel like the right place for this.
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
		if c := i.Constants[member]; c != nil {
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
