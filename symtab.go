package main
import (
	"fmt"
	"bytes"
	"strings"
)

//----------------------------------------------------------------------------------------------------------------------

const ptrSize = 8 // 64-bit pointer size in bytes
const fnPrefix = "clara_"
//----------------------------------------------------------------------------------------------------------------------

var byteType = &Type{ Kind: Byte, Data: &IntType{ Width: 1 } }
var intType = &Type{ Kind: Integer, Data: &IntType{ Width: 8 } }
var boolType = &Type{ Kind: Boolean, Data: &BoolType{ Width: 8 } }
var stringType = &Type{ Kind: String, Data: &StringType{ Width: 8 } }
var nothingType = &Type{ Kind: Nothing, Data: &NothingType{ Width: 0 } }

//----------------------------------------------------------------------------------------------------------------------

type TypeKind byte
const (
	Struct = TypeKind(iota)
	Function
	Integer
	Byte
	Boolean
	String
	Array
	Nothing
)

var typeKindNames = map[TypeKind]string {
	Struct:   "struct",
	Function: "function",
	Integer:  "int",
	Byte:     "byte",
	Boolean:  "bool",
	String:   "string",
	Array:    "[]",
	Nothing:   "nothing",
}

func (tk TypeKind) String() string {
	s, ok := typeKindNames[tk]
	if !ok {
		s = "<unknown type kind>"
	}
	return s
}

//----------------------------------------------------------------------------------------------------------------------

type Type struct {
	Kind TypeKind
	Data interface{}
}

func (t *Type) Matches(x *Type) bool {
	switch t.Kind {
	case Struct:
		return x.Kind == Struct && t.AsStruct().Name == x.AsStruct().Name
	case Integer, Byte:
		return x.Kind == Integer || x.Kind == Byte // Int & bytes can be used interchangeably...
	case Boolean, String, Nothing:
		return t.Kind == x.Kind
	case Array:
		if x.Kind != Array {
			return false
		}
		te := t.AsArray().Elem
		xe := x.AsArray().Elem
		// SPECIAL CASE: Int & Byte are _not_ interchangeable in arrays!
		if te.Kind == Integer || te.Kind == Byte {
			return te.Kind == xe.Kind
		} else {
			return te.Matches(xe)
		}
	case Function:
		if x.Kind != Function  {
			return false
		}
		xf := x.AsFunction()
		tf := t.AsFunction()
		if len(xf.Args) != len(tf.Args) {
			return false
		}
		for i, arg := range xf.Args {
			if !arg.Matches(tf.Args[i]) {
				return false
			}
		}
		return tf.ret.Matches(xf.ret)
	default:
		panic("Unknown or unexpected type comparison!")
	}
}

func (t *Type) Is(kind TypeKind) bool {
	return t.Kind == kind
}

func (t *Type) IsArray(kind TypeKind) bool {
	return t.Is(Array) && t.AsArray().Elem.Is(kind)
}

func (t *Type) IsFunction(kind TypeKind) bool {
	return t.Is(Function) && t.AsFunction().ret.Is(kind)
}

func (t *Type) IsPointer() bool {
	return t.Is(Array) || t.Is(Struct) || t.Is(String) || t.Is(Function)
}

func (t *Type) AsStruct() *StructType {
	return t.Data.(*StructType)
}

func (t *Type) AsFunction() *FunctionType {
	return t.Data.(*FunctionType)
}

func (t *Type) AsArray() *ArrayType {
	return t.Data.(*ArrayType)
}

func (t *Type) String() string {
	switch t.Kind {
	case Array: return t.Kind.String() + t.AsArray().Elem.String()
	case Struct: return t.AsStruct().Name
	case Function:
		var types []string
		fn := t.AsFunction()
		for _, t := range fn.Args {
			types = append(types, t.String())
		}
		return fmt.Sprintf("fn(%v) %v", strings.Join(types, ","), fn.ret.String())
	default:
		return t.Kind.String()
	}
}

func (t *Type) GcName() string {
	if !t.IsPointer() {
		panic("Non-pointer types cannot have GC functions!")
	}
	return fmt.Sprintf("%v%v_gc", fnPrefix, t.AsmName())
}

func (t *Type) AsmName() string {
	switch t.Kind {
	case Array: return fmt.Sprintf("array$%v$", t.AsArray().Elem.AsmName())
	case Struct: return t.AsStruct().Name
	case Function:
		fn := t.AsFunction()
		buf := bytes.NewBufferString("fn")
		if len(fn.Args) > 0 {
			buf.WriteString(fmt.Sprintf("$%v", fn.Args[0].AsmName()))
			for i := 1; i < len(fn.Args); i += 1 {
				buf.WriteString(fmt.Sprintf(".%v", fn.Args[i].AsmName()))
			}
			buf.WriteString("$")
		}
		return buf.String()
	default:
		return t.Kind.String()
	}
}

func (t *Type) Width() int {
	switch x := t.Data.(type) {
	case *IntType: return x.Width
	case *BoolType: return x.Width
	case *StructType, *StringType, *ArrayType: return ptrSize
	default:
		panic(fmt.Sprintf("Type.Width() called for unknown data type: %T", t.Data))
	}
}

//----------------------------------------------------------------------------------------------------------------------

type StructType struct {
	Name string
	Fields []*Symbol
}

func (st *StructType) GetField(name string) *Symbol {
	off := 0
	for _, field := range st.Fields {
		if field.Name == name {
			return field
		}
		off += ptrSize
	}
	return nil // Not found
}

func (st *StructType) HasField(name string) bool {
	return st.GetField(name) != nil
}

func (st *StructType) Size() int {
	return len(st.Fields) * ptrSize
}

//----------------------------------------------------------------------------------------------------------------------

type FunctionType struct {
	Args          []*Type
	ret           *Type
	gcFunc        string  // stores name of GC func for closure
	isVariadic    bool
	isConstructor bool
	IsExternal 	  bool  // Provided at linktime - no code gen required
}

// Used during codegen to avoid clashes with shared library functions
func (ft *FunctionType) AsmName(name string) string {
	if ft.IsExternal {
		return name
	}
	if name == "main" {
		return fnPrefix + "main"
	}

	// Build name safe for usage in ASM
	buf := bytes.NewBufferString(fnPrefix)
	buf.WriteString(name)
	for _, arg := range ft.Args {
		buf.WriteString(".")
		buf.WriteString(arg.AsmName())
	}
	return buf.String()
}

func (ft *FunctionType) Describe(name string) string {
	if ft.IsExternal {
		return fmt.Sprintf("%v (external)", name)
	}

	// Build name safe for usage in ASM
	buf := bytes.NewBufferString(name)
	buf.WriteString("(")
	var types []string
	for _, arg := range ft.Args {
		types = append(types, arg.String())
	}
	buf.WriteString(strings.Join(types, ", "))
	buf.WriteString(")")
	return buf.String()
}

//----------------------------------------------------------------------------------------------------------------------

type ArrayType struct {
	Elem *Type
}

//----------------------------------------------------------------------------------------------------------------------

type IntType struct {
	Width int
}

//----------------------------------------------------------------------------------------------------------------------

type StringType struct {
	Width int
}

//----------------------------------------------------------------------------------------------------------------------

type BoolType struct {
	Width int
}

//----------------------------------------------------------------------------------------------------------------------

type NothingType struct {
	Width int
}
//----------------------------------------------------------------------------------------------------------------------

type Symbol struct {
	Name      string
	Addr      int
	IsStack   bool
	IsGlobal  bool
	IsLiteral bool
	Type      *Type
	Next 	  *Symbol // Only valid for function symbols!
}

//----------------------------------------------------------------------------------------------------------------------

type SymTab struct {
	parent *SymTab
	children []*SymTab
	symbols map[string]*Symbol
}

//----------------------------------------------------------------------------------------------------------------------

func NewSymtab() *SymTab {
	return &SymTab{nil, nil, make(map[string]*Symbol)}
}

func (st *SymTab) Define(s *Symbol) (*Symbol, bool) {
	if s, ok := st.symbols[s.Name]; ok {
		return s, true
	}
	st.symbols[s.Name] = s
	return s, false
}

func (st *SymTab) OwnedBy(s *Symbol) bool {
	_, ok := st.symbols[s.Name]
	return ok
}

func (st *SymTab) Resolve(name string) (*Symbol, bool) {
	s, ok := st.symbols[name]
	if !ok && st.parent != nil {
		s, ok = st.parent.Resolve(name)
	}
	return s, ok
}

func (st *SymTab) MustResolve(name string) *Symbol {
	s, ok := st.Resolve(name)
	if !ok {
		panic(fmt.Sprintf("Required symbol '%v' not found!", name))
	}
	return s
}

func (st *SymTab) Parent() *SymTab {
	return st.parent
}

func (st *SymTab) Child() *SymTab {
	child := NewSymtab()
	child.parent = st
	st.children = append(st.children, child)
	return child
}

func (st *SymTab) Walk(f func(*Symbol)) {
	// Walk children first then this node
	for _, child := range st.children {
		child.Walk(f)
	}
	for _, s := range st.symbols {
		f(s)
	}
}

// Unique list of all types in table
func (st *SymTab) allTypes() []*Type {
	t := make(map[string]*Type)
	st.Walk(func(s *Symbol) {
		x := s.Type.AsmName()
		if _, ok := t[x]; !ok {
			t[x] = s.Type
		}
	})
	var typs []*Type
	for _, typ := range t {
		typs = append(typs, typ)
	}
	return typs
}