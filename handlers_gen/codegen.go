package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"text/template"
)

type tpl struct {
	FieldName string
}

var (
	intTpl = template.Must(template.New("intTpl").Parse(`
	// {{.FieldName}}
	var {{.FieldName}}Raw uint32
	binary.Read(r, binary.LittleEndian, &{{.FieldName}}Raw)
	in.{{.FieldName}} = int({{.FieldName}}Raw)
`))

	strTpl = template.Must(template.New("strTpl").Parse(`
	// {{.FieldName}}
	var {{.FieldName}}LenRaw uint32
	binary.Read(r, binary.LittleEndian, &{{.FieldName}}LenRaw)
	{{.FieldName}}Raw := make([]byte, {{.FieldName}}LenRaw)
	binary.Read(r, binary.LittleEndian, &{{.FieldName}}Raw)
	in.{{.FieldName}} = string({{.FieldName}}Raw)
`))
)

func main() {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	out, _ := os.Create(os.Args[2])

	fmt.Fprintln(out, `package `+node.Name.Name)
	fmt.Fprintln(out) // empty line
	fmt.Fprintln(out, `import "encoding/binary"`)
	fmt.Fprintln(out, `import "bytes"`)
	fmt.Fprintln(out) // empty line

	for _, f := range node.Decls {
		g, ok := f.(*ast.GenDecl)
		if !ok {
			fmt.Printf("SKIP %T is not *ast.GenDecl\n", f)
			continue
		}
		for _, spec := range g.Specs {
			currType, ok := spec.(*ast.TypeSpec)
			if !ok {
				fmt.Printf("SKIP %T is not ast.TypeSpec\n", spec)
				continue
			}

			currStruct, ok := currType.Type.(*ast.StructType)
			if !ok {
				fmt.Printf("SKIP %T is not ast.StructType\n", currStruct)
				continue
			}

			putValidateFunction := false

			for _, field := range currStruct.Fields.List {

				if field.Tag != nil {

					println("tags", field.Tag.Value)
					tag := reflect.StructTag(field.Tag.Value[1 : len(field.Tag.Value)-1])
					validatorTag, ok := tag.Lookup("apivalidator")
					if ok {
						if !putValidateFunction {
							putValidateFunction = true
							fmt.Fprintf(out, "func ( input * %s) validate () bool { \n", currType.Name)
						}
						vInfo := getValidationInfo(validatorTag)
						if (vInfo.required) {
							fmt.Fprintf(out, "if input.%s == \"\" {return false} \n", field.Names[0].Name)
						}

					}

				}

			}
			if putValidateFunction {
				fmt.Fprint(out, "return true \n}\n")
			}

		}
	}
}

func getValidationInfo(tag string) validationInfo {
	result := validationInfo{}
	for _, param := range strings.Split(tag, ",") {
		pairs := strings.Split(param, "")
		key := pairs[0]
		value := pairs[1]
		switch key {
		case "required":
			result.required = true
		case "paramname":
			result.paramname = value
		case "enum":
			result.enumValues = strings.Split(value, "|")
		case "default":
			result.defaultValue = value
		case "min":
			result.hasMin = true
			result.min, _ = strconv.Atoi(value)
		case "max":
			result.hasMax = true
			result.max, _ = strconv.Atoi(value)
		}
	}
	return result
}

type validationInfo struct {
	required     bool
	hasMin       bool
	hasMax       bool
	min          int
	max          int
	paramname    string
	enumValues   []string
	defaultValue string
}
