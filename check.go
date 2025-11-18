package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"mibk.dev/phpfmt/token"
)

func Check(x any) {
	l := linter{
		stdout:           os.Stdout,
		scope:            make(map[string]string),
		fileBeingChecked: "<line>",
	}
	l.check(x)
}

type linter struct {
	stdout io.Writer
	scope  map[string]string

	// TODO: Fix this.
	fileBeingChecked string

	thisClass *Class
}

func (l *linter) reportf(pos token.Pos, format string, args ...any) {
	fmt.Fprintf(l.stdout, "%s:%d:%d: %s\n",
		l.fileBeingChecked, pos.Line, pos.Column,
		fmt.Sprintf(format, args...),
	)
}

func (l *linter) check(x any) {
	dump := json.NewEncoder(os.Stdout)
	_ = dump
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *File:
		l.fileBeingChecked = x.Path
		l.check(x.Scope)
	case *Class:
		l.thisClass = x
	case *scope:
		for _, p := range x.Params {
			l.scope[p.Name] = p.Class
		}
		for _, stmt := range x.Stmts {
			l.check(stmt)
		}
	case *Debug:
		class := l.scope[x.Var]
		if class != "" {
			l.reportf(x.Pos, "%v is of type: %v (DEBUG)", x.Var, class)
		} else {
			l.reportf(x.Pos, "unknown var: %v (DEBUG)", x.Var)
		}
	case *NewInstance:
		// no check
	case *Stmt:
		for _, n := range x.Nodes {
			l.check(n)
		}
	case *AssignExpr:
		l.check(x.Left)
		if checked := l.findVarType(x); !checked {
			l.check(x.Right)
		}
	case *MemberAccess:
		// dump.Encode(x)
		l.checkMemberAccess(x)
	case *VarExpr:
		// dump.Encode(x)
	}
}

func (l *linter) findVarType(a *AssignExpr) (checked bool) {
	v, ok := a.Left.(*VarExpr)
	if !ok {
		return false
	}

	class := "<unknown-val>"
	switch val := a.Right.(type) {
	case *NewInstance:
		class = val.Class
	case *VarExpr:
		class = cmp.Or(l.scope[val.Name], class)
	case *MemberAccess:
		class = l.checkMemberAccess(val)
		checked = true
	}

	if class == "void" {
		l.reportf(a.Right.Pos(), "cannot assign '%s' to %s", class, v.Name)
		class = "stdClass"
	}

	l.scope[v.Name] = class
	return checked
}

func (l *linter) checkMemberAccess(a *MemberAccess) string {
	var x string
	switch r := a.Rcvr.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", r))
	case *VarExpr:
		if r.Name == "$this" && l.thisClass != nil {
			x = l.thisClass.Name
		} else {
			x = cmp.Or(l.scope[r.Name], "<unknown-type-of-"+r.Name+">")
		}
	case *MemberAccess:
		x = l.checkMemberAccess(r)
	}

	if isBasicType(x) {
		l.reportf(a.Pos(), "cannot call method on '%s'", x)
		return "<not-a-class>"
	}

	if x = strings.TrimPrefix(x, `\`); x == "stdClass" {
		// All member access allowed.
		return x
	}
	return l.checkClassMember(a.Pos(), x, a.Name)
}

func (l *linter) checkClassMember(pos token.Pos, class, member string) string {
	// TODO: Different error if entity exists but is not a class?
	c, ok := universe[class].(*Class)
	if !ok {
		l.reportf(pos, "class `%v` not found", class)
		return "\\stdClass"
	}

	for _, name := range c.Traits {
		t, ok := universe[name].(*Trait)
		if !ok {
			l.reportf(pos, "trait `%v` not found", name)
			continue
		}
		for _, m := range t.Members {
			// TODO: Check whether member not defined?
			c.addMember(m)
		}
	}
	c.Traits = nil // Mark as process.

	m, ok := c.Members[member]
	for !ok && c.Extends != "" {
		parent := strings.TrimPrefix(c.Extends, `\`)
		return l.checkClassMember(pos, parent, member)
	}
	if !ok {
		l.reportf(pos, "class member `%v::%v` does not exist", c.Name, member)
		return "\\stdClass"
	}
	return m.Class
}
