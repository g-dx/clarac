package main

import (
	"errors"
	"fmt"
	"strings"
	"strconv"
	"github.com/g-dx/clarac/lex"
)

const (
	syntaxErrMsg = "%v:%d:%d: syntax error, Unexpected '%v', Expecting: '%v'"
	errRedeclaredMsg = "%v:%d:%d: error, '%v' redeclared"
)

type Parser struct {
	pos    int
	tokens []*lex.Token
	errs   []error
	discard bool // Are we in "discard" mode?
	symtab *SymTab
	extra []*Node
}

var errUnexpectedEof = errors.New("Unexpected EOF")

func NewParser(tokens []*lex.Token, nodes []*Node, syms []Symbol) *Parser {
	// Add any symbols from predefined nodes
	symtab := NewSymtab()
	for _, n := range nodes {
		symtab.Define(n.sym)
	}
	// Add any global symbols
	for _, s := range syms {
		symtab.Define(s)
	}
	return &Parser{tokens : tokens, symtab: symtab, extra : nodes}
}

func (p *Parser) Parse() (errs []error, root *Node) {

	// Setup handler to recover from unexpected EOF
	defer p.onUnexpectedEof(&errs)

	// Create root & loop over stream
	root = &Node{op : opRoot, symtab: p.symtab}
	for p.isNot(lex.EOF) {
		if p.is(lex.Fn) {
			root.Add(p.fnDeclaration())
		} else {
			p.syntaxError(lex.Fn, lex.EOF)
			p.next()
		}
	}

	// Add extra nodes
	// TODO: This really should come before we parse but the code generator requires them to be last
	for _, n := range p.extra {
		root.Add(n)
	}

	return p.errs, root
}

// ==========================================================================================================
// Grammar-implementing functions

func (p *Parser) fnDeclaration() *Node {

	// Open new symtab
	p.symtab = p.symtab.Child()

	// Match declaration
	p.need(lex.Fn)
	name := p.need(lex.Identifier)
	p.need('(')
	params := p.parseParameters()
	p.need(')')
	// TODO: Decide if return types are optional
	var retType *lex.Token
	if p.isNot(lex.LBrace) {
		retType = p.need(lex.Identifier)
	}
	p.need('{')
	stmts := p.parseStatements()

	// Close symtab
	syms := p.symtab
	p.symtab = p.symtab.Parent()
	return p.fnDclNode(name, params, stmts, syms, retType)
}

func (p *Parser) parseStatements() []*Node {

	var stmts []*Node
	for p.isNot('}') {
		stmts = append(stmts, p.parseStatement())
	}
	p.need('}')
	return stmts
}

func (p *Parser) parseStatement() *Node {

	switch p.Kind() {
	case lex.Return: return p.parseReturnExpr()
	case lex.Identifier:
		// TODO: Assumes all identifiers beginning statements are function calls!
		return p.parseFnCall()
	default:
		p.syntaxError(lex.Identifier, lex.Return)
		p.next()
		return &Node{op: opError} // TODO: Bad statement node?
	}
}

func (p *Parser) parseReturnExpr() *Node {

	tok := p.need(lex.Return)
	return &Node{op:opReturn, stats: []*Node{ p.parseExpr() }, token: tok }
}

func (p *Parser) parseParameters() []*Node {
	var params []*Node = nil
	for p.isNot(lex.EOF, ')') {
		params = append(params, p.parseParameter())
		for p.match(',') {
			params = append(params, p.parseParameter())
		}
	}
	return params
}

func (p *Parser) parseParameter() *Node {
	ident := p.need(lex.Identifier)
	var typ *lex.Token
	if p.is(lex.Colon) {
		// Type is also declared
		p.next()
		typ = p.need(lex.Identifier)
	}
	// TODO: Is where a better AST would help. We could record both position of the identifier and the optional type.
	v := &VarSymbol{ val: ident.Val }
	p.symtab.Define(v)
	return &Node{token: ident, op: opIdentifier, typ: typ, sym: v, symtab: p.symtab}
}

func (p *Parser) parseArgs() (n []*Node) {
	p.need('(')
	if p.isNot(lex.EOF, ')') {
		n = append(n, p.parseArg())
		for p.match(',') {
			n = append(n, p.parseArg())
		}
	}
	p.need(')')
	return n
}

func (p *Parser) parseArg() (*Node) {
	z := 2 + 1
	print(z)
	return p.parseExpr()
}

func (p *Parser) parseExpr() (*Node) {

	lhs := p.parseOperand()
	// TODO: Is this correct? What are all the terminators for an expression?
	for p.isNot(lex.Comma, lex.RParen, lex.RBrace, lex.Return) {
		op, tok := p.parseOperator()
		rhs := p.parseOperand()
		lhs = &Node{ op: op, left: lhs, right: rhs, token: tok, symtab: p.symtab}
	}
	return lhs
}

func (p *Parser) parseOperand() *Node {
	switch p.Kind() {
	case lex.Integer:
		return p.parseIntegerLit()
	case lex.String:
		return p.parseStringLit()
	case lex.Identifier:
		return p.parseIdentifierOrFnCall()
	default:
		p.syntaxError(lex.Integer, lex.String)
		return &Node{op: opError} // TODO: Maybe "opMissingOperand" would be better
	}
}

func (p *Parser) parseIdentifierOrFnCall() *Node {
	// TODO: All logic from parseIdentifier() & parseFnCall() has been duplicated here!
	tok := p.need(lex.Identifier)
	switch p.Kind() {
	case lex.LParen:
		return p.fnCallNode(tok, p.parseArgs())
	default:
		return &Node { op: opIdentifier, token: tok, symtab: p.symtab }
	}
}

func (p *Parser) parseIdentifier() *Node {
	tok := p.need(lex.Identifier)
	// Define symbol
	// TODO: Define symbol???
	return &Node { op: opIdentifier, token: tok, sym: nil, symtab: p.symtab} // ???
}

func (p *Parser) parseOperator() (int, *lex.Token) {
	if p.isNot(lex.Plus) {
		p.syntaxError(lex.Plus)
		p.next()
		return opError, nil
	}

	return opIntAdd, p.next()
}

func (p *Parser) parseIntegerLit() (*Node) {
	// Match first arg
	arg := p.next()
	// Define symbol
	sym, found := p.symtab.Resolve(symIntegerLit, arg.Val)
	if !found {
		i, err := strconv.ParseInt(arg.Val, 10, 64)
		if err != nil {
			panic(err) // Should never happen
		}
		sym = &IntegerLiteralSymbol{val : i }
		p.symtab.Define(sym)
	}
	return &Node{token : arg, op : opIntLit, sym : sym, symtab: p.symtab}
}

func (p *Parser) parseStringLit() (*Node) {
	// Match first arg
	arg := p.next()

	// Define symbol
	sym, found := p.symtab.Resolve(symStrLit, arg.Val)
	if !found {
		sym = &StringLiteralSymbol{val : arg.Val }
		p.symtab.Define(sym)
	}
	return &Node{token : arg, op : opStrLit, sym : sym, symtab: p.symtab}
}

// ==========================================================================================================
// AST Node functions

func (p *Parser) fnDclNode(token *lex.Token, args []*Node, stmts []*Node, syms *SymTab, returnTyp *lex.Token) *Node {

	// Check symtab for redeclare
	sym, found := p.symtab.Resolve(symFnDecl, token.Val)
	if found {
		p.symbolError(errRedeclaredMsg, token)
	} else {
		sym = &Function{token.Val, len(args), false, 0, syms, nil}
		p.symtab.Define(sym) // Functions don't take params yet
	}
	return &Node{token : token, args: args, stats : stmts, op : opFuncDcl, sym : sym, symtab: p.symtab, typ: returnTyp}
}

func (p *Parser) parseFnCall() *Node {
	return p.fnCallNode(p.need(lex.Identifier), p.parseArgs())
}

func (p *Parser) fnCallNode(token *lex.Token, args []*Node) *Node {
	// TODO: TEMPORARY WORKAROUND!
	sym, _ := p.symtab.Resolve(symFnDecl, token.Val)
	return &Node{token : token, stats : args, op : opFuncCall, sym : sym, symtab: p.symtab}
}

// ==========================================================================================================
// Matching & movement functions

func (p *Parser) need(k lex.Kind) *lex.Token {
	for !p.is(k) {
		fmt.Printf(lex.KindValues[p.tokens[p.pos].Kind])
		p.syntaxError(k)
		p.next()
	}
	p.discard = false
	return p.next()
}

func (p *Parser) isNot(kinds...lex.Kind) bool {
	for _, k := range kinds {
		if p.is(k) {
			return false
		}
	}
	return true
}

func (p *Parser) is(kinds...lex.Kind) bool {
	for _, kind := range kinds {
		if p.Kind() == kind {
			return true
		}
	}
	return false
}

func (p *Parser) Kind() lex.Kind {
	return p.tokens[p.pos].Kind
}

func (p *Parser) match(k lex.Kind) bool {
	if p.is(k) {
		p.next()
		p.discard = false
		return true
	}
	return false
}

func (p *Parser) next() *lex.Token {
	// Panic if unexpectedly no more input
	if (p.pos+1 >= len(p.tokens)) {
		panic(errUnexpectedEof)
	}
	token := p.tokens[p.pos]
	p.pos++
	return token
}

func (p *Parser) symbolError(err string, token *lex.Token) {
	p.errs = append(p.errs,
		errors.New(fmt.Sprintf(err,
			token.File,
			token.Line,
			token.Pos,
			token.Val)))
}

func (p *Parser) syntaxError(expected...lex.Kind) {
	if !p.discard {
		// Enable discard mode
		p.discard = true

		// Gather values
		expectedValues := make([]string, 0, 2)
		for _, v := range expected {
			expectedValues = append(expectedValues, lex.KindValues[v])
		}

		// Store error
		token := p.tokens[p.pos]
		p.errs = append(p.errs,
			errors.New(fmt.Sprintf(syntaxErrMsg,
				token.File,
				token.Line,
				token.Pos,
				p.tokens[p.pos].Val,
				strings.Join(expectedValues, "' or '"))))
	}
}

func (p *Parser) onUnexpectedEof(errs *[]error) {
	if r := recover(); r != nil {
		*errs = p.errs
		if r != errUnexpectedEof {
			panic(r)
		}
	}
}