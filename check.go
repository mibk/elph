package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func check(x any) {
	dump := json.NewEncoder(os.Stdout)
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *File:
		check(x.Scope)
	case *scope:
		for _, stmt := range x.Nodes {
			check(stmt)
		}
	case *stmt:
		for _, n := range x.Nodes {
			check(n)
		}
	case *MemberAccess:
		dump.Encode(x)
	case *VarExpr:
		// dump.Encode(x)
	}
}

/*
func (p *parser) parseMemberAccess() (_ Expr) {
	if x == "$this" {
		x = p.thisClass
	}

	allAllowed := false
	for p.got(token.Arrow) {
		if tok := p.tok; p.got(token.Ident) {
			if allAllowed || x == "stdClass" {
				allAllowed = true
				continue
			}

			if ns, rest, ok := strings.Cut(x, "\\"); ok {
				if tr, ok := p.use[ns]; ok {
					x = tr + "\\" + rest
				}
			}

			c, ok := world[x]
			if !ok {
				log.Printf("class `%v` not found", x)
				return
			}
			m, ok := c.Members[tok.Text]
			for !ok && c.Extends != "" {
				p := c.Extends
				c, ok = world[p]
				if !ok {
					log.Printf("parent `%v` not found; searching for %v", p, tok.Text)
					return
				}
				m, ok = c.Members[tok.Text]
			}
			if !ok {
				log.Printf("member `%v` not found", tok.Text)
				return
			}

			x = getClass(m.Type)
		}
	}
}
*/
