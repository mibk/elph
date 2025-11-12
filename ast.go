package main

import (
	"mibk.dev/phpfmt/phpdoc/phptype"
	"mibk.dev/phpfmt/token"
)

type File struct {
	Scope *scope
}

type scope struct {
	Kind        token.Type
	Open, close token.Type
	Nodes       []*stmt
}

type stmt struct {
	Kind  token.Type
	Nodes []any
}

type Class struct {
	Name    string
	Extends string // or empty
	Members map[string]*Member
}

type Member struct {
	Name string
	Type phptype.Type
}
