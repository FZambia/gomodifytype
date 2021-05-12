package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// structType contains a structType node and it's name. It's a convenient
// helper type, because *ast.StructType doesn't contain the name of the struct
type structType struct {
	name string
	node *ast.StructType
}

type config struct {
	file       string
	write      bool
	structName string
	fieldName  string
	line       string
	start      int
	end        int
	all        bool
	from       string
	to         string

	skipUnexportedFields bool

	fileSet *token.FileSet
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	err = cfg.validate()
	if err != nil {
		return err
	}

	node, err := cfg.parse()
	if err != nil {
		return err
	}

	start, end, err := cfg.findSelection(node)
	if err != nil {
		return err
	}

	rewrittenNode, err := cfg.rewrite(node, start, end)
	if err != nil {
		return err
	}

	out, err := cfg.format(rewrittenNode)
	if err != nil {
		return err
	}

	if !cfg.write {
		fmt.Println(out)
	}
	return nil
}

func parseConfig(args []string) (*config, error) {
	var (
		flagFile   = flag.String("file", "", "Filename to be parsed")
		flagWrite  = flag.Bool("w", false, "Write result to source file instead of stdout")
		flagLine   = flag.String("line", "", "Line number of the field or a range of line. i.e: 4 or 4,8")
		flagStruct = flag.String("struct", "", "Struct name to be processed")
		flagField  = flag.String("field", "", "Field name to be processed")
		flagAll    = flag.Bool("all", false, "Select all structs to be processed")
		flagFrom   = flag.String("from", "", "From type")
		flagTo     = flag.String("to", "", "To type")

		flagSkipUnexportedFields = flag.Bool("skip-unexported", false, "Skip unexported fields")
	)

	// this fails if there are flags re-defined with the same name.
	if err := flag.CommandLine.Parse(args); err != nil {
		return nil, err
	}

	if flag.NFlag() == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		return nil, flag.ErrHelp
	}

	cfg := &config{
		file:                 *flagFile,
		line:                 *flagLine,
		structName:           *flagStruct,
		fieldName:            *flagField,
		all:                  *flagAll,
		write:                *flagWrite,
		from:                 *flagFrom,
		to:                   *flagTo,
		skipUnexportedFields: *flagSkipUnexportedFields,
	}

	return cfg, nil
}

func (c *config) parse() (ast.Node, error) {
	c.fileSet = token.NewFileSet()
	return parser.ParseFile(c.fileSet, c.file, nil, parser.ParseComments)
}

// findSelection returns the start and end position of the fields that are
// suspect to change. It depends on the line or struct selection.
func (c *config) findSelection(node ast.Node) (int, int, error) {
	if c.line != "" {
		return c.lineSelection(node)
	} else if c.structName != "" {
		return c.structSelection(node)
	} else if c.all {
		return c.allSelection(node)
	} else {
		return 0, 0, errors.New("-line, -struct or -all is not passed")
	}
}

// collectStructs collects and maps structType nodes to their positions
func collectStructs(node ast.Node) map[token.Pos]*structType {
	structs := make(map[token.Pos]*structType)

	collectStructs := func(n ast.Node) bool {
		var t ast.Expr
		var structName string

		switch x := n.(type) {
		case *ast.TypeSpec:
			if x.Type == nil {
				return true

			}

			structName = x.Name.Name
			t = x.Type
		case *ast.CompositeLit:
			t = x.Type
		case *ast.ValueSpec:
			structName = x.Names[0].Name
			t = x.Type
		case *ast.Field:
			// this case also catches struct fields and the structName
			// therefore might contain the field name (which is wrong)
			// because `x.Type` in this case is not a *ast.StructType.
			//
			// We're OK with it, because, in our case *ast.Field represents
			// a parameter declaration, i.e:
			//
			//   func test(arg struct {
			//   	Field int
			//   }) {
			//   }
			//
			// and hence the struct name will be `arg`.
			if len(x.Names) != 0 {
				structName = x.Names[0].Name
			}
			t = x.Type
		}

		// if expression is in form "*T" or "[]T", dereference to check if "T"
		// contains a struct expression
		t = deref(t)

		x, ok := t.(*ast.StructType)
		if !ok {
			return true
		}

		structs[x.Pos()] = &structType{
			name: structName,
			node: x,
		}
		return true
	}

	ast.Inspect(node, collectStructs)
	return structs
}

func (c *config) format(file ast.Node) (string, error) {
	var buf bytes.Buffer
	err := format.Node(&buf, c.fileSet, file)
	if err != nil {
		return "", err
	}

	if c.write {
		err = ioutil.WriteFile(c.file, buf.Bytes(), 0)
		if err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

func (c *config) lineSelection(_ ast.Node) (int, int, error) {
	var err error
	parts := strings.Split(c.line, ",")

	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}

	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, err
		}
	}

	if start > end {
		return 0, 0, errors.New("wrong range. start line cannot be larger than end line")
	}

	return start, end, nil
}

func (c *config) structSelection(file ast.Node) (int, int, error) {
	structs := collectStructs(file)

	var encStruct *ast.StructType
	for _, st := range structs {
		if st.name == c.structName {
			encStruct = st.node
		}
	}

	if encStruct == nil {
		return 0, 0, errors.New("struct name does not exist")
	}

	// if field name has been specified as well, only select the given field
	if c.fieldName != "" {
		return c.fieldSelection(encStruct)
	}

	start := c.fileSet.Position(encStruct.Pos()).Line
	end := c.fileSet.Position(encStruct.End()).Line

	return start, end, nil
}

func (c *config) fieldSelection(st *ast.StructType) (int, int, error) {
	var encField *ast.Field
	for _, f := range st.Fields.List {
		for _, field := range f.Names {
			if field.Name == c.fieldName {
				encField = f
			}
		}
	}

	if encField == nil {
		return 0, 0, fmt.Errorf("struct %q doesn't have field name %q",
			c.structName, c.fieldName)
	}

	start := c.fileSet.Position(encField.Pos()).Line
	end := c.fileSet.Position(encField.End()).Line

	return start, end, nil
}

// allSelection selects all structs inside a file
func (c *config) allSelection(file ast.Node) (int, int, error) {
	start := 1
	end := c.fileSet.File(file.Pos()).LineCount()

	return start, end, nil
}

func isPublicName(name string) bool {
	for _, c := range name {
		return unicode.IsUpper(c)
	}
	return false
}

// rewrite rewrites the node for structs between the start and end
// positions
func (c *config) rewrite(node ast.Node, start, end int) (ast.Node, error) {
	rewriteFunc := func(n ast.Node) bool {
		x, ok := n.(*ast.StructType)
		if !ok {
			return true
		}

		for _, f := range x.Fields.List {
			line := c.fileSet.Position(f.Pos()).Line

			if !(start <= line && line <= end) {
				continue
			}

			fieldName := ""
			if len(f.Names) != 0 {
				for _, field := range f.Names {
					if !c.skipUnexportedFields || isPublicName(field.Name) {
						fieldName = field.Name
						break
					}
				}
			}

			// anonymous field
			if f.Names == nil {
				ident, ok := f.Type.(*ast.Ident)
				if !ok {
					continue
				}

				if !c.skipUnexportedFields {
					fieldName = ident.Name
				}
			}

			// nothing to process, continue with next line
			if fieldName == "" {
				continue
			}

			typeString := types.ExprString(f.Type)
			if typeString == c.from {
				f.Type = ast.NewIdent(c.to)
			}
		}

		return true
	}

	ast.Inspect(node, rewriteFunc)

	c.start = start
	c.end = end

	return node, nil
}

// validate validates whether the config is valid or not
func (c *config) validate() error {
	if c.file == "" {
		return errors.New("no file is passed")
	}

	if c.line == "" && c.structName == "" && !c.all {
		return errors.New("-line, -struct or -all is not passed")
	}

	if c.line != "" && c.structName != "" {
		return errors.New("-line or -struct cannot be used together. pick one")
	}

	if c.fieldName != "" && c.structName == "" {
		return errors.New("-field is requiring -struct")
	}

	return nil
}

// deref takes an expression, and removes all its leading "*" and "[]"
// operator. Use case : if found expression is a "*t" or "[]t", we need to
// check if "t" contains a struct expression.
func deref(x ast.Expr) ast.Expr {
	switch t := x.(type) {
	case *ast.StarExpr:
		return deref(t.X)
	case *ast.ArrayType:
		return deref(t.Elt)
	}
	return x
}
