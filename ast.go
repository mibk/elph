package main

import "mibk.dev/phpfmt/token"

type File struct {
	htmlPreamble *token.Token
	scope        *scope
}

type scope struct {
	kind        token.Type
	open, close token.Type
	nodes       []*stmt
}

type stmt struct {
	kind  token.Type
	nodes []any
}
