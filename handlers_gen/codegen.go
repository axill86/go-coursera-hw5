package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

)

type validationInfo struct {
	required         bool
	hasMin           bool
	hasMax           bool
	min              int
	max              int
	replaceParamName bool
	paramname        string
	enumValues       []string
	useDefault bool
	defaultValue     string
}

type apigenComment struct {
	URL    string `json:"url"`
	Auth   bool   `json:"auth"`
	Method string `json:"method"`
}

func main() {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	out, _ := os.Create(os.Args[2])

	fmt.Fprintln(out, `// Generated code `)
	fmt.Fprintln(out, `package `+node.Name.Name)
	fmt.Fprintln(out) // empty line
	fmt.Fprintln(out, `import "net/http"`)
	fmt.Fprintln(out, `import "strconv"`)
	fmt.Fprintln(out, `import "fmt"`)

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

			CreateFillFunction(out, currType.Name.Name, currStruct.Fields.List)
		}
	}
}

func getValidationInfo(tag string) validationInfo {
	result := validationInfo{}
	for _, param := range strings.Split(tag, ",") {
		if strings.HasPrefix(param, "required") {
			result.required = true
			continue
		}
		pairs := strings.Split(param, "=")
		key := pairs[0]
		value := pairs[1]

		switch key {
		case "paramname":
			result.replaceParamName = true
			result.paramname = value
		case "enum":
			result.enumValues = strings.Split(value, "|")
		case "default":
			result.useDefault = true
			result.defaultValue = value
		case "min":
			result.hasMin = true
			result.min, _ = strconv.Atoi(value)
		case "max":
			result.hasMax = true
			result.max, _ = strconv.Atoi(value)
		}
	}
	fmt.Println(result)
	return result
}

func CreateFillFunction(out io.Writer, structName string, fields []*ast.Field) {
	fmt.Fprintf(out, "// Generating Bind Function for struct %s\n", structName)
	fmt.Fprintf(out, "func (model *%s) Bind(r *http.Request) error {\n", structName)

	for _, field := range fields {

		if field.Tag != nil {

			tag := reflect.StructTag(field.Tag.Value[1 : len(field.Tag.Value)-1])
			validatorTag, ok := tag.Lookup("apivalidator")
			if ok {
				vInfo := getValidationInfo(validatorTag)
				paramName := field.Names[0].Name
				if vInfo.replaceParamName {
					paramName = vInfo.paramname
				}

				switch field.Type.(*ast.Ident).Name {
				case "string":
					fmt.Fprintf(out, "model.%s = r.FormValue(\"%s\") \n", field.Names[0].Name, paramName)
					ValidateStringField(out, field, vInfo)
					if vInfo.useDefault {
						fmt.Fprintf(out, `if model.%s == "" {
			model.%s = "%s"
		}
`, field.Names[0].Name, field.Names[0].Name, vInfo.defaultValue)
					}
				case "int":
					fmt.Fprintf(out, "model.%s, _ = strconv.Atoi(r.FormValue(\"%s\")) \n", field.Names[0].Name, paramName)
					ValidateIntField(out, field, vInfo)
				}

			}

		}

	}
	fmt.Fprintln(out, "return nil")
	fmt.Fprint(out, "}\n")
}

func ValidateStringField(out io.Writer, field *ast.Field, vInfo validationInfo) {
	println ("validation for field ", field.Names[0].Name)
	if vInfo.required {
		fmt.Fprintf(out, `
if model.%s == "" {
		return fmt.Errorf("Field %s is required")
	}
`, field.Names[0].Name, field.Names[0].Name)
	}

	if vInfo.hasMax {
		fmt.Fprintf(out, `
if len(model.%s) > %d {
		return fmt.Errorf("Field %s should not be longer than %d")
	}
`, field.Names[0].Name, vInfo.max, field.Names[0].Name, vInfo.max)
	}

	if vInfo.hasMin {
		fmt.Fprintf(out, `
if len(model.%s) < %d {
		return fmt.Errorf("Field %s should not be shorter than %d")
	}
`, field.Names[0].Name, vInfo.min, field.Names[0].Name, vInfo.min)
	}

	if len(vInfo.enumValues) > 0 {
		fmt.Fprintf(out, "switch model.%s {\n", field.Names[0].Name)
		for _, enumValue := range vInfo.enumValues {
			fmt.Fprintf(out, "case \"%s\":\n", enumValue)
		}
		if vInfo.useDefault {
			fmt.Fprint(out, "case \"\":\n")
		}
		fmt.Fprintf(out, `default:
		return fmt.Errorf("Field %s should contain one of the following %v")
	}
`, field.Names[0].Name, vInfo.enumValues)
	}

}

func ValidateIntField(out io.Writer, field *ast.Field, vInfo validationInfo) {
	if vInfo.required {
		fmt.Fprintf(out, `
if model.%s == 0 {
		return fmt.Errorf("Field %s is required")
	}
`, field.Names[0].Name, field.Names[0].Name)
	}
	if vInfo.hasMax {
		fmt.Fprintf(out, `
if model.%s > %d {
		return fmt.Errorf("Field %s should not be  larger than %d")
	}
`, field.Names[0].Name, vInfo.max, field.Names[0].Name, vInfo.max)
	}

	if vInfo.hasMin {
		fmt.Fprintf(out, `
if model.%s < %d {
		return fmt.Errorf("Field %s should be smaller than %d")
	}
`, field.Names[0].Name, vInfo.min, field.Names[0].Name, vInfo.min)
	}
}