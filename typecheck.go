package main

import (
	"fmt"
	"strings"
	"github.com/g-dx/clarac/console"
)

//---------------------------------------------------------------------------------------------------------------

var fn *FunctionType // Function which is currently being type checked

func typeCheck(n *Node, debug bool) (errs []error) {

	left := n.left
	right := n.right

	switch n.op {
	case opIf, opElseIf:
		errs = append(errs, typeCheck(left, debug)...)

		if !left.hasType() {
			goto end
		}

		if !left.typ.Is(Boolean) {
			// TODO: More specific message for if statement?
			errs = append(errs, semanticError2(errMismatchedTypesMsg, left.token, left.typ.Name(), boolType.Name()))
			goto end
		}

		// Type check body
		for _, stmt := range n.stmts {
			errs = append(errs, typeCheck(stmt, debug)...)
		}

		// Type check next elseif case (if any)
		if right != nil {
			errs = append(errs, typeCheck(right, debug)...)
		}

		// Does not promote type...

	case opElse:
		// Type check body
		for _, stmt := range n.stmts {
			errs = append(errs, typeCheck(stmt, debug)...)
		}

		// Does not promote type...

	case opReturn:
		// "Empty" return
		if left == nil {
			n.typ = nothingType
			return errs
		}

		errs = append(errs, typeCheck(left, debug)...)
		if !left.hasType() {
			goto end
		}

		if !fn.ret.Matches(left.typ) {
			errs = append(errs, semanticError2(errMismatchedTypesMsg, left.token, left.typ.Name(), fn.ret.Name()))
			goto end
		}
		n.typ = left.typ


	case opAnd, opOr, opAdd, opMul, opMin, opDiv:
		errs = append(errs, typeCheck(left, debug)...)
		errs = append(errs, typeCheck(right, debug)...)

		if !left.hasType() || !right.hasType() {
			goto end
		}

		if !operatorTypes.isValid(n.op, left.typ.Kind) {
			// Not valid for op
			errs = append(errs, semanticError2(errInvalidOperatorTypeMsg, left.token, left.typ.Name(), n.token.Val))
			goto end
		}
		if !operatorTypes.isValid(n.op, right.typ.Kind) {
			// Not valid for op
			errs = append(errs, semanticError2(errInvalidOperatorTypeMsg, right.token, right.typ.Name(), n.token.Val))
			goto end
		}
		if !left.typ.Matches(right.typ) {
			// Mismatched types
			errs = append(errs, semanticError2(errMismatchedTypesMsg, left.token, left.typ.Name(), right.typ.Name()))
		}

		n.typ = left.typ

	case opNot:
		errs = append(errs, typeCheck(left, debug)...)

		if !left.hasType() {
			goto end
		}

		if !left.typ.Is(Boolean) {
			// TODO: More specific message for if statement?
			errs = append(errs, semanticError2(errMismatchedTypesMsg, left.token, left.typ.Name(), boolType.Name()))
			goto end
		}
		n.typ = left.typ

	case opLit:
		n.typ = n.sym.Type

	case opIdentifier:
		// If no symbol - try to find identifier declaration
		if n.sym == nil {
			sym, found := n.symtab.Resolve(n.token.Val)
			if !found {
				errs = append(errs, semanticError(errUnknownVarMsg, n.token))
				goto end
			}
			n.sym = sym
		}
		n.typ = n.sym.Type

	case opFuncCall:
		// Check exists
		s, found := n.symtab.Resolve(n.token.Val)
		if !found {
			// Undefined
			errs = append(errs, semanticError(errUndefinedMsg, n.token))
			goto end
		}

		// Check is a function
		if !s.Type.Is(Function) {
			errs = append(errs, semanticError(errNotFuncMsg, n.token))
			goto end
		}

		// Type check any default parameters
		// TODO: This is required as forward declarations mean the function itself may not have been type checked yet. The
		// solution to this is to do two passes over the AST, first type check all top level functions and then type check the bodies.
		fn := s.Type.AsFunction()
		for _, def := range fn.Defaults {
			if def != nil {
				errs = append(errs, typeCheck(def, debug)...)
			}
		}

		// Type check args
		for _, stmt := range n.stmts {
			errs = append(errs, typeCheck(stmt, debug)...)
			if !stmt.hasType() {
				goto end
			}
		}

		// Check we have _at least_ enough parameters to call this function
		if len(n.stmts) < fn.MandatoryParams() {
			errs = append(errs, semanticError2(errNotEnoughArgsMsg, n.token, n.token.Val))
			goto end
		}

		// TODO: Support variadic functions!
		if !fn.isVariadic {

			// Check for too many arguments supplied
			if len(n.stmts) > len(fn.Args) {
				errs = append(errs, semanticError2(errTooManyArgsMsg, n.stmts[0].token, n.token.Val))
				goto end
			}

			// 1. Check that supplied parameter type order is compatible with function signature
			pos := 0
		paramTypes:
			for _, param := range n.stmts {
				for ; pos < len(fn.Args); pos += 1 {

					// 1a. Parameter type matches supplied argument
					if fn.Args[pos].Type.Name() == param.typ.Name() {
						pos +=1
						continue paramTypes
					}

					// 1b. Supplied argument type doesn't match and no default available
					if fn.Args[pos].Type.Name() != param.typ.Name() && fn.Defaults[pos] == nil {
						errs = append(errs, semanticError2(errMismatchedTypesMsg, param.token, param.typ.Name(), fn.Args[pos].Type.Name()))
						goto end
					}
				}

				// 1c. Matching supplied parameter types to function signature types has failed
				var types []string = nil
				for _, p := range n.stmts {
					types = append(types, p.typ.Name())
				}
				errs = append(errs, semanticError2(errCannotApplyArgsMsg, n.stmts[0].token, n.token.Val, strings.Join(types, ", ")))
				goto end
			}

			// 2. Copy supplied args across while leaving space for defaults
			args := make([]*Node, len(fn.Args))
			pos = 0
		outer:
			for i := 0; i < len(n.stmts); i += 1 {
				for ; pos < len(fn.Args); pos += 1 {
					if fn.Args[pos].Type.Matches(n.stmts[i].typ) {
						args[pos] = n.stmts[i]
						pos +=1
						continue outer
					}
				}
			}

			// 3. Move supplied args to correct positions & fill in any remaining gaps with default values
			defaults := make([]*Node, len(fn.Args))
			argsSyms := make(map[*Symbol]*Node)
			for i := len(args) - 1; i >= 0; i -= 1 {
				if args[i] != nil {
					argsSyms[fn.Args[i]] = args[i]
				} else {
					if fn.Defaults[i] != nil {
						// 3b. If empty and we have a default, use it. Step 4 will rewrite any variable references.
						args[i] = copyNode(fn.Defaults[i])
						defaults[i] = args[i]
					} else {
						// 3b. If empty and no default, "pull" nearest parameter of correct type up to this position
						var p *Node
						for j := i - 1; j >= 0; j -= 1 {
							if args[j] != nil {
								p = args[j]
								args[j] = nil
								break
							}
						}
						args[i] = p
						argsSyms[fn.Args[i]] = args[i]
					}
				}
			}

			// 4. Rewrite all parameter references in any default expressions to use supplied argument AST nodes
			for i := 0; i < len(defaults); i += 1 {
				if defaults[i] != nil {
					replaceNode(defaults[i], func (x *Node) *Node {
						if expr, ok := argsSyms[x.sym]; ok {
							return expr
						}
						return nil
					})
					argsSyms[fn.Args[i]] = defaults[i] // Make visible for further default rewrites...
				}
			}

			// 5. Set all arguments
			n.stmts = args
		}

		// Finally set symbol on node
		n.sym = s
		n.typ = fn.ret

	case opGt, opLt, opEq:
		errs = append(errs, typeCheck(left, debug)...)
		errs = append(errs, typeCheck(right, debug)...)

		if !left.hasType() || !right.hasType() {
			goto end
		}
		if !left.typ.Matches(right.typ) {
			errs = append(errs, semanticError2(errMismatchedTypesMsg, left.token, left.typ.Name(), right.typ.Name()))
			goto end
		}
		n.typ = boolType

	case opFuncDcl:
		// Type check params & default values
		for _, param := range n.params {
			errs = append(errs, typeCheck(param, debug)...)
			if param.left != nil {
				errs = append(errs, typeCheck(param.left, debug)...)
			}
		}

		// Type check stmts
		for _, stmt := range n.stmts {
			errs = append(errs, typeCheck(stmt, debug)...)
		}

		n.typ = n.sym.Type

	case opDot:
		errs = append(errs, typeCheck(left, debug)...)

		if !left.hasType() {
			goto end
		}

		// Handle func call on right
		if right.op == opFuncCall {

			// Rewrite to func call
			n.op = opFuncCall
			n.token = right.token
			n.symtab = right.symtab
			n.stmts = append([]*Node{n.left}, right.stmts...)
			n.left = nil
			n.right = nil

			// Type check func call
			errs = append(errs, typeCheck(n, debug)...)

			// Handle field access on right
		} else if right.op == opIdentifier {

			// SPECIAL CASE: Fudge strings to give them a special int field "length" at offset 0
			// TODO: Add arrays here too when required
			if (left.sym.Type.Is(Array) || left.sym.Type.Is(String)) && right.token.Val == "length" {
				right.sym = &Symbol{Name: "length", Addr: 0, Type: intType}
				right.typ = right.sym.Type
				n.typ = right.typ
				return errs
			}

			// Check we have a struct
			if !left.sym.Type.Is(Struct) {
				errs = append(errs, semanticError(errNotStructMsg, left.token))
				goto end
			}

			// Check field exists in struct
			strct := left.sym.Type.AsStruct()
			sym, offset := strct.Offset(right.token.Val)
			if sym == nil {
				errs = append(errs, semanticError(errStructHasNoFieldMsg, right.token, strct.Name))
				goto end
			}

			// Set field offset
			// TODO: This whole process process isn't necessary because when we build a StructType we can set the offsets
			// for each symbol
			sym.Addr = offset

			// Set right symbol and set parent as right
			right.sym = sym
			right.typ = sym.Type

			n.sym = right.sym
			n.typ = right.typ

		} else {
			// Unexpected type on right
			errs = append(errs, semanticError(errInvalidDotSelectionMsg, right.token))
			goto end
		}


	case opArray:
		errs = append(errs, typeCheck(left, debug)...)
		errs = append(errs, typeCheck(right, debug)...)

		if !left.hasType() || !right.hasType() {
			goto end
		}

		if !right.typ.Is(Integer) {
			errs = append(errs, semanticError2(errNonIntegerIndexMsg, right.token, right.typ.Name()))
			goto end
		}
		n.typ = left.typ

		// SPECIAL CASE: If the left type is a string, array access yields a byte
		if left.typ.Is(String) {
			n.typ = byteType
		}

	case opDas:
		errs = append(errs, typeCheck(right, debug)...)

		if !right.hasType() {
			goto end
		}

		// Check we have identifier on left
		// TODO: Should we attempt to type check left to get more information?
		if left.op != opIdentifier {
			errs = append(errs, semanticError2(errUnexpectedAssignMsg, left.token))
		}

		// Left gets type of right
		left.sym.Type = right.typ
		left.typ = right.typ

		// Does not promote type...

	case opAs:
		errs = append(errs, typeCheck(right, debug)...)
		errs = append(errs, typeCheck(left, debug)...)

		if !right.hasType() || !left.hasType() {
			goto end
		}

		// Check left is addressable
		if !left.isAddressable() {
			errs = append(errs, semanticError2(errNotAddressableAssignMsg, left.token))
			goto end
		}

		// Check types in assignment
		leftType := left.typ
		if leftType.Is(Array) {
			leftType = leftType.AsArray().Elem
		}
		if !leftType.Matches(right.typ) {
			errs = append(errs, semanticError2(errMismatchedTypesMsg, right.token, right.typ.Name(), leftType.Name()))
			goto end
		}

		// Does not promote type...

	case opRoot:
		panic("Type check called on root node of AST")

	case opError:
		// TODO: Decide what to do here...
		goto end

	default:
		panic(fmt.Sprintf("Node type [%v] not processed during type check!", nodeTypes[n.op]))
	}

	// DEBUG
	if debug {
		printTypeInfo(n)
	}

end:
	return errs
}

//---------------------------------------------------------------------------------------------------------------

func printTypeInfo(n *Node) {
	// TODO: Fix the type name printing!
	calculatedType := "<EMPTY>"
	if n.typ != nil {
		calculatedType = n.typ.String()
	}

	location := fmt.Sprintf("%v:%d:%d", n.token.File, n.token.Line, n.token.Pos)
	if n.token.File == "" {
		location = "<AST defined>"
	}
	symbolName := strings.Replace(n.token.Val, "%", "%%", -1) // Escape Go format strings
	if n.op != opLit {
		symbolName = "\"" + symbolName + "\""
	}

	// Dump type info
	fmt.Printf(debugTypeInfoFormat,
		console.Yellow, location, console.Disable,
		console.Red, fmt.Sprintf("%s(%s)", nodeTypes[n.op], symbolName), console.Disable,
		console.Green, calculatedType, console.Disable)
}