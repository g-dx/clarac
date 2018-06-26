package main

import (
	"fmt"
	"os"
    "strings"
    "io/ioutil"
    "flag"
	"path/filepath"
	"github.com/g-dx/clarac/lex"
	"os/exec"
	"os/user"
	"io"
	"errors"
	"runtime"
)

func main() {

	// Get user details
	usr, err := user.Current()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Default install dir
	defaultInstall := fmt.Sprintf("%v/.clara", usr.HomeDir)

	// Load program path. Default to "examples"
	installPath := flag.String("install", defaultInstall, "Path to install directory.")
	progPath := flag.String("prog", "/examples/hello.clara", "File with Clara program to compile.")
	showProg := flag.Bool("in", false, "Print the input program.")
	showLex := flag.Bool("lex", false, "Print the lexical output.")
	showAst := flag.Bool("ast", false, "Print the generated AST.")
	showTypes := flag.Bool("types", false, "Print type information as it assigned during semantic analysis.")
	showAsm := flag.Bool("asm", false, "Print the generated assembly (intel syntax).")
	outPath := flag.String("out", ".", "Path to write program to.")
	flag.Parse()

	// Gather standard lib & C files
	claraLib := glob(fmt.Sprintf("%v/lib/*.clara", *installPath)) // NOTE: Does NOT traverse all directories!
	cLib := glob(fmt.Sprintf("%v/init/*.c", *installPath)) // NOTE: Does NOT traverse all directories!

	options := options{ showLex: *showLex, showAst: *showAst, showTypes: *showTypes, showAsm: *showAsm, showProg: *showProg }
	_, errs := Compile(options, claraLib, *progPath, cLib, *outPath, os.Stdout)
	if len(errs) > 0 {
		fmt.Println("\nErrors")
		for _, err := range errs {
			fmt.Printf(" - %v\n", err)
		}
		os.Exit(1)
	}
}

type options struct {
	showLex   bool
	showAst   bool
	showTypes bool
	showAsm   bool
	showProg  bool
}

func Compile(options options, claraLibPaths []string, progPath string, cLibPaths []string, outPath string, out io.Writer) (string, []error) {

	// Define root AST node
	rootSymtab := NewSymtab()
	rootNode := &Node{op: opRoot, symtab: rootSymtab}

	// Add "AST defined" nodes & symbols
	for _, n := range stdlib() {
		rootSymtab.Define(n.sym)
		rootNode.Add(n)
	}

	// Add any global symbols
	for _, s := range stdSyms() {
		rootSymtab.Define(s)
	}

	// Lex + parse all Clara files
	var errs []error
	claraLibPaths = append(claraLibPaths, progPath)
	for _, f := range claraLibPaths {
		bytes, err := ioutil.ReadFile(f)
		if err != nil {
			return "", []error{err}
		}
		errs = append(errs, lexAndParse(string(bytes), f, rootNode, options.showLex, out)...)
	}
	if len(errs) > 0 {
		return "", errs
	}

	// Handle top level types first
	errs = append(errs, processTopLevelTypes(rootNode, rootSymtab)...)
	if len(errs) > 0 {
		return "", errs
	}

	// Generate constructor functions
	errs = append(errs, walk(postOrder, rootNode, rootSymtab, rootNode, generateStructConstructors)...)
	errs = append(errs, walk(postOrder, rootNode, rootSymtab, rootNode, addRuntimeInit)...)
	errs = append(errs, walk(preOrder, rootNode, rootSymtab, rootNode, foldConstants)...)
	if len(errs) > 0 {
		return "", errs
	}

	// Type check
	errs = append(errs, typeCheck(rootNode, rootSymtab, nil, options.showTypes)...)
	if len(errs) > 0 {
		return "", errs
	}

	// Show final AST if necessary
	if options.showAst {
		printTree(rootNode, out)
	}

	// Create assembly file
	basename := filepath.Base(progPath)
	progName := strings.TrimSuffix(basename, filepath.Ext(basename))
	asmPath := fmt.Sprintf("%v/%v.S", os.TempDir(), progName)
	os.Remove(asmPath) // Ignore error
	f, err := os.Create(asmPath)
	if err != nil {
		return "", []error{err}
	}

	// Generate assembly
	asm := NewGasWriter(f, options.showAsm)
	err = codegen(rootSymtab, rootNode.stmts, asm)
	if err != nil {
		return "", []error{errors.New(fmt.Sprintf("\nCode Gen Errors:\n %v\n", err))}
	}
	f.Close()

	// Invoke gcc to link files
	outputPath := filepath.Join(outPath, progName)
	args := []string { "-fno-pie" }
	if runtime.GOOS == "linux" {
		args = append(args, "-no-pie")
	}
	args = append(args, "-o")
	args = append(args, outputPath)
	args = append(args, asmPath)
	args = append(args, cLibPaths...)
	cmd := exec.Command("gcc", args...)
	cmd.Stderr = os.Stderr
	stdOut, err := cmd.Output()
	if err != nil {
		return "", []error{errors.New(fmt.Sprintf("Link failure: %v\n%v\n", err, string(stdOut)))}
	}
	return outputPath, nil
}

func lexAndParse(code string, path string, root *Node, showLex bool, out io.Writer) (errs []error) {

	// Lex
	var tokens []*lex.Token
	lexer := lex.Lex(code, path)
	// TODO: Lexing errors should really appear from parse stage
	for {
		token := lexer.NextToken()
		// TODO: Parser could filter tokens it's not interested in
		switch token.Kind {
		case lex.EOL, lex.Space, lex.Comment:
			continue
		case lex.Err:
			return []error { errors.New(token.String()) }
		default:
			tokens = append(tokens, token)
		}
		// Check for EOF
		if token.Kind == lex.EOF {
			break
		}
	}

	if showLex {
		printLex(tokens, out)
	}

	// Parse
	return NewParser().Parse(tokens, root)
}

func stdSyms() []*Symbol {
	return []*Symbol{
		// string type
		{ Name: "string", Type: stringType },
		// int type
		{ Name: "int", Type: intType },
		// byte type
		{ Name: "byte", Type: byteType },
		// bool type
		{ Name: "bool", Type: boolType },
		// nothing type
		{ Name: "nothing", Type: nothingType },
	}
}

func stdlib() []*Node {
	return []*Node{
		// printf (from libc)
		{token:&lex.Token{Val : "printf"}, op: opBlockFnDcl,
		sym:&Symbol{ Name: "printf", IsGlobal: true, Type: &Type{ Kind: Function, Data:
			&FunctionType{ Args: []*Type { stringType }, isVariadic: true, ret: nothingType, IsExternal: true }}}},

		// debug (from runtime.c)
		{token:&lex.Token{Val : "debug"}, op: opBlockFnDcl,
			sym:&Symbol{ Name: "debug", IsGlobal: true, Type: &Type{ Kind: Function, Data:
			&FunctionType{ Args: []*Type { stringType, stringType }, isVariadic: true, ret: nothingType, IsExternal: true }}}},
	}
}

func printLex(tokens []*lex.Token, out io.Writer) {
	fmt.Fprintln(out, "\nLexical Tokens")
	for _, token := range tokens {
		fmt.Fprintln(out, token)
	}
}

func glob(pattern string) []string {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		panic(err) // Only happens with bad pattern
	}
	return paths
}