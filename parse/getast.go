package parse

import (
	"fmt"
	"github.com/philhofer/msgp/gen"
	"github.com/ttacon/chalk"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
)

var (
	// this records a set of all the
	// identifiers in the file that are
	// not go builtins. identities not
	// in this set after the first pass
	// of processing are "unknown" identifiers.
	globalIdents map[string]gen.Base

	// this records the set of all
	// processed types (types for which we created code)
	globalProcessed map[string]struct{}
)

func init() {
	globalIdents = make(map[string]gen.Base)
	globalProcessed = make(map[string]struct{})
}

// GetAST simply creates the ast out of a filename and filters
// out non-exported elements.
func GetAST(filename string) (files []*ast.File, pkgName string, err error) {
	var (
		f     *ast.File
		fInfo os.FileInfo
	)

	fset := token.NewFileSet()
	fInfo, err = os.Stat(filename)
	if err != nil {
		return
	}
	if fInfo.IsDir() {
		var pkgs map[string]*ast.Package
		pkgs, err = parser.ParseDir(fset, filename, nil, parser.AllErrors)
		if err != nil {
			return
		}

		// we'll assume one package per dir
		var pkg *ast.Package
		for _, pkg = range pkgs {
			pkgName = pkg.Name
		}
		files = make([]*ast.File, 0, len(pkg.Files))
		for _, file := range pkg.Files {
			files = append(files, file)
		}
		return
	}

	f, err = parser.ParseFile(fset, filename, nil, parser.AllErrors)
	if err != nil {
		return
	}
	if !ast.FileExports(f) {
		f, err = nil, fmt.Errorf("no exported definitions in %s", filename)
	}
	files = []*ast.File{f}
	if f != nil {
		pkgName = f.Name.Name
	}
	return
}

// GetElems opens the file at "filename", extracts all of the
// type specifications, and converts each "supported" type spec (*ast.StructType)
// to a gen.Elem. (Also returns the package name as a string.)
//
// May return emtpy values if there are no useful specs, etc.
//
func GetElems(filename string) ([]gen.Elem, string, error) {
	f, pkg, err := GetAST(filename)
	if err != nil {
		return nil, "", err
	}

	specs := make([]*ast.TypeSpec, 0, len(f))
	for _, file := range f {
		specs = append(specs, GetTypeSpecs(file)...)
	}
	if len(specs) == 0 {
		return nil, pkg, nil
	}

	out := make([]gen.Elem, 0, len(specs))
	for i := range specs {
		el := GenElem(specs[i])
		if el != nil {
			out = append(out, el)
		}
	}

	var ptd bool
	for _, o := range out {
		unr := findUnresolved(o)
		if len(unr) > 0 {
			if !ptd {
				fmt.Println(chalk.Yellow.Color("Non-local or unresolved identifiers:"))
				ptd = true
			}
			for _, u := range unr {
				fmt.Printf(chalk.Yellow.Color(" -> %q\n"), u)
			}
		}
	}

	return out, pkg, nil
}

// GetTypeSpecs extracts all of the *ast.TypeSpecs in the file.
func GetTypeSpecs(f *ast.File) []*ast.TypeSpec {
	var out []*ast.TypeSpec

	// check all declarations...
	for i := range f.Decls {

		// for GenDecls...
		if g, ok := f.Decls[i].(*ast.GenDecl); ok {

			// and check the specs...
			for _, s := range g.Specs {

				// for ast.TypeSpecs....
				if ts, ok := s.(*ast.TypeSpec); ok {
					out = append(out, ts)

					// record identifier
					switch ts.Type.(type) {
					case *ast.StructType:
						globalIdents[ts.Name.Name] = gen.IDENT

					case *ast.Ident:
						// we will resolve this later
						globalIdents[ts.Name.Name] = pullIdent(ts.Type.(*ast.Ident).Name)

					case *ast.ArrayType:
						a := ts.Type.(*ast.ArrayType)
						switch a.Elt.(type) {
						case *ast.Ident:
							if a.Elt.(*ast.Ident).Name == "byte" {
								globalIdents[ts.Name.Name] = gen.Bytes
							} else {
								globalIdents[ts.Name.Name] = gen.IDENT
							}
						default:
							globalIdents[ts.Name.Name] = gen.IDENT
						}

					case *ast.StarExpr:
						globalIdents[ts.Name.Name] = gen.IDENT

					case *ast.MapType:
						globalIdents[ts.Name.Name] = gen.IDENT

					}
				}
			}
		}
	}
	return out
}

// GenElem creates the gen.Elem out of an
// ast.TypeSpec. Right now the only supported
// TypeSpec.Type is *ast.StructType. Unsupported
// types will yield a 'nil' return value.
func GenElem(in *ast.TypeSpec) gen.Elem {
	// handle supported types
	switch in.Type.(type) {

	case *ast.StructType:
		v := in.Type.(*ast.StructType)
		fmt.Printf(chalk.Green.Color("parsing %s..."), in.Name.Name)
		p := &gen.Ptr{
			Value: &gen.Struct{
				Name:   in.Name.Name, // ast.Ident
				Fields: parseFieldList(v.Fields),
			},
		}

		// mark type as processed
		globalProcessed[in.Name.Name] = struct{}{}

		if len(p.Value.(*gen.Struct).Fields) == 0 {
			fmt.Printf(chalk.Red.Color(" has no exported fields \u2717\n")) // X
			return nil
		}
		fmt.Print(chalk.Green.Color("  \u2713\n")) // check
		return p

	default:
		return nil

	}
}

// this is where most of the magic happens
func parseFieldList(fl *ast.FieldList) []gen.StructField {
	if fl == nil || fl.NumFields() == 0 {
		return nil
	}
	out := make([]gen.StructField, 0, fl.NumFields())

for_fields:
	for _, field := range fl.List {
		var sf gen.StructField
		// field name

		switch len(field.Names) {
		case 1:
			sf.FieldName = field.Names[0].Name
		case 0:
			sf.FieldName = embedded(field.Type)
			if sf.FieldName == "" {
				// means it's a selector expr., or
				// something else unsupported
				fmt.Printf(chalk.Yellow.Color(" (\u26a0 field %v unsupported)"), field.Type)
				continue for_fields
			}
		default:
			// inline multiple field declaration
			for _, nm := range field.Names {
				el := parseExpr(field.Type)
				if el == nil {
					// skip
					fmt.Printf(chalk.Yellow.Color(" (\u26a0 field %q unsupported)"), sf.FieldName)
					continue for_fields
				}

				out = append(out, gen.StructField{
					FieldTag:  nm.Name,
					FieldName: nm.Name,
					FieldElem: el,
				})
			}
			continue for_fields
		}

		// field tag
		var flagExtension bool
		if field.Tag != nil {
			// we need to trim the leading and trailing ` characters for
			// to convert to reflect.StructTag
			body := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("msg")

			tags := strings.Split(body, ",")
			switch len(tags) {
			case 2:
				// special case: explicit Extension conversion as `msg:"{name},extension"`
				if tags[1] == "extension" {
					flagExtension = true
				} else {
					fmt.Printf(chalk.Yellow.Color(" (\u26a0 unknown tag %q)"), tags[1])
				}
			case 3:
				// special case: explicit type shim as `msg:"{name},as:{type},using:{to}/{from}"`
				if strings.HasPrefix(tags[1], "as:") && strings.HasPrefix(tags[2], "using:") {
					if tp, to, from := parseShim(tags[1], tags[2]); to != "" && from != "" {
						sf.FieldTag = tags[0]
						sf.FieldElem = &gen.BaseElem{
							Value:        tp,
							Convert:      true,
							ShimToBase:   to,
							ShimFromBase: from,
						}
						out = append(out, sf)
					} else {
						fmt.Printf(chalk.Yellow.Color("  (\u26a0 couldn't parse: %q)"), body)
					}
					continue for_fields
				}
			}
			sf.FieldTag = tags[0]
		}
		if sf.FieldTag == "" {
			sf.FieldTag = sf.FieldName
		} else if sf.FieldTag == "-" {
			// deliberately ignore field
			continue for_fields
		}

		e := parseExpr(field.Type)
		if e == nil {
			// unsupported type
			fmt.Printf(chalk.Yellow.Color(" (\u26a0 field %q unsupported)"), sf.FieldName)
			continue
		}

		// mark as extension
		if flagExtension {
			// an extension can be
			// a pointer or base type
			switch e.Type() {
			case gen.PtrType:
				if e.Ptr().Value.Type() == gen.BaseType {
					e.Ptr().Value.Base().Value = gen.Ext
				} else {
					fmt.Printf(chalk.Yellow.Color(" (\u26a0 field %q couldn't be cast as an extension"), sf.FieldName)
					continue
				}
			case gen.BaseType:
				e.Base().Value = gen.Ext
			default:
				fmt.Printf(chalk.Yellow.Color(" (\u26a0 field %q couldn't be cast as an extension"), sf.FieldName)
				continue
			}
		}

		sf.FieldElem = e
		out = append(out, sf)
	}
	return out
}

// extract embedded field name
func embedded(f ast.Expr) string {
	switch f.(type) {
	case *ast.Ident:
		return f.(*ast.Ident).Name
	case *ast.StarExpr:
		return embedded(f.(*ast.StarExpr).X)
	default:
		// other possibilities (like selector expressions)
		// are disallowed; we can't reasonably know
		// their type
		return ""
	}
}

// recursively translate ast.Expr to gen.Elem; nil means type not supported
// expected input types:
// - *ast.MapType (map[T]J)
// - *ast.Ident (name)
// - *ast.ArrayType ([(sz)]T)
// - *ast.StarExpr (*T)
// - *ast.StructType (struct {})
// - *ast.SelectorExpr (a.B)
// - *ast.InterfaceType (interface {})
func parseExpr(e ast.Expr) gen.Elem {
	switch e.(type) {

	case *ast.MapType:
		m := e.(*ast.MapType)
		if k, ok := m.Key.(*ast.Ident); ok && k.Name == "string" {
			if in := parseExpr(m.Value); in != nil {
				return &gen.Map{Value: in}
			}
		}
		return nil

	case *ast.Ident:
		b := &gen.BaseElem{
			Value: pullIdent(e.(*ast.Ident).Name),
		}
		if b.Value == gen.IDENT {
			b.Ident = (e.(*ast.Ident).Name)
		}
		return b

	case *ast.ArrayType:
		arr := e.(*ast.ArrayType)

		// special case for []byte
		if arr.Len == nil {
			if i, ok := arr.Elt.(*ast.Ident); ok && i.Name == "byte" {
				return &gen.BaseElem{Value: gen.Bytes}
			}
		}

		// return early if we don't know
		// what the slice element type is
		els := parseExpr(arr.Elt)
		if els == nil {
			return nil
		}

		// array and not a slice
		if arr.Len != nil {
			switch arr.Len.(type) {
			case *ast.BasicLit:
				return &gen.Array{
					Size: arr.Len.(*ast.BasicLit).Value,
					Els:  els,
				}

			case *ast.Ident:
				return &gen.Array{
					Size: arr.Len.(*ast.Ident).String(),
					Els:  els,
				}

			default: // TODO: support *ast.SelectorExpr
				return nil
			}
		}
		return &gen.Slice{Els: els}

	case *ast.StarExpr:
		if v := parseExpr(e.(*ast.StarExpr).X); v != nil {
			return &gen.Ptr{Value: v}
		}
		return nil

	case *ast.StructType:
		if fields := parseFieldList(e.(*ast.StructType).Fields); len(fields) > 0 {
			return &gen.Struct{Fields: fields}
		}
		return nil

	case *ast.SelectorExpr:
		v := e.(*ast.SelectorExpr)
		// special case for time.Time; others go to Ident
		if im, ok := v.X.(*ast.Ident); ok {
			if v.Sel.Name == "Time" && im.Name == "time" {
				return &gen.BaseElem{Value: gen.Time}
			} else {
				return &gen.BaseElem{
					Value: gen.IDENT,
					Ident: im.Name + "." + v.Sel.Name,
				}
			}
		}
		return nil

	case *ast.InterfaceType:
		// support `interface{}`
		if len(e.(*ast.InterfaceType).Methods.List) == 0 {
			return &gen.BaseElem{Value: gen.Intf}
		}
		return nil

	default: // other types not supported
		return nil
	}
}

// parse shim: "as:{type}", "using:{toShim}/{fromShim}""
func parseShim(as string, using string) (tp gen.Base, toShim string, fromShim string) {
	tp = pullIdent(strings.TrimPrefix(as, "as:"))
	lrs := strings.Split(strings.TrimPrefix(using, "using:"), "/")
	if len(lrs) == 2 {
		toShim, fromShim = lrs[0], lrs[1]
	}
	return
}

func pullIdent(name string) gen.Base {
	switch name {
	case "string":
		return gen.String
	case "[]byte":
		return gen.Bytes
	case "byte":
		return gen.Byte
	case "int":
		return gen.Int
	case "int8":
		return gen.Int8
	case "int16":
		return gen.Int16
	case "int32":
		return gen.Int32
	case "int64":
		return gen.Int64
	case "uint":
		return gen.Uint
	case "uint8":
		return gen.Uint8
	case "uint16":
		return gen.Uint16
	case "uint32":
		return gen.Uint32
	case "uint64":
		return gen.Uint64
	case "bool":
		return gen.Bool
	case "float64":
		return gen.Float64
	case "float32":
		return gen.Float32
	case "complex64":
		return gen.Complex64
	case "complex128":
		return gen.Complex128
	case "time.Time":
		return gen.Time
	case "interface{}":
		return gen.Intf
	case "msgp.Extension", "Extension":
		return gen.Ext
	default:
		// unrecognized identity
		return gen.IDENT
	}
}
