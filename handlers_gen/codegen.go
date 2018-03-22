package main

import (
	"encoding/json"
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
	useDefault       bool
	defaultValue     string
}

type handlerInfo struct {
	apigenInfo    apigenInfo
	hanlderMethod string
}
type apigenInfo struct {
	URL     string `json:"url"`
	Auth    bool   `json:"auth"`
	Method  string `json:"method"`
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
	fmt.Fprintln(out, `import "encoding/json"`)
	fmt.Fprint(out, `
type Body struct {
	Error    string      "json:\"error\""
	Response interface{} "json:\"response,omitempty\""
}
`)
	fmt.Fprintln(out) // empty line

	handlers := make(map[string][]handlerInfo)
	for _, f := range node.Decls {
		switch f.(type) {
		case *ast.GenDecl:
			g, _ := f.(*ast.GenDecl)
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

				CreateBindFunction(out, currType.Name.Name, currStruct.Fields.List)
			}
		case *ast.FuncDecl:
			fun, _ := f.(*ast.FuncDecl)
			if strings.HasPrefix(fun.Doc.Text(), "apigen:api") {
				jsonStr := strings.TrimPrefix(fun.Doc.Text(), "apigen:api")

				var apiInfo apigenInfo

				err := json.Unmarshal([]byte(jsonStr), &apiInfo)
				if err != nil {
					log.Fatalf("Can't parse apigen instructions %s", jsonStr)
				}

				fmt.Fprintf(out, "//Generated Handler for function %s\n", fun.Name.Name)
				receiverName := fun.Recv.List[0].Type.(*ast.StarExpr).X.(*ast.Ident).Name
				paramsName := fun.Type.Params.List[len(fun.Type.Params.List)-1].Type.(*ast.Ident).Name
				handlerName := fmt.Sprintf("%sHandler", fun.Name)
				handlers[receiverName] = append(handlers[receiverName], handlerInfo{apigenInfo: apiInfo, hanlderMethod: handlerName})
				fmt.Fprintf(out, `
func (srv *%s) %s(w http.ResponseWriter, r *http.Request) {

`, receiverName, handlerName)
				if apiInfo.Auth {
					fmt.Fprint(out, `
if r.Header.Get("X-Auth") != "100500" {
		w.WriteHeader(403)
		body := Body{
				Error: "unauthorized",
			}
		bodyBytes, _:= json.Marshal(body)
		w.Write(bodyBytes)
		return
	}
`)
				}
				if apiInfo.Method != "" {
					fmt.Fprintf(out, `
if r.Method != "%s" {
		w.WriteHeader(406)
		body := Body {
			Error: "bad method",
		}
		bodyBytes, _:= json.Marshal(body)
		w.Write(bodyBytes)
		return
	}
`, apiInfo.Method)
				}

				fmt.Fprintf(out, `

	params := %s{}
	err := params.Bind(r)
	w.Header()["Content-Type"] = []string{"application/json"}
	if err != nil {
		w.WriteHeader(400)
		body := Body {
			Error: err.Error(),
		}
		bodyBytes, _:= json.Marshal(body)
		w.Write(bodyBytes)
		return
	}
`, paramsName)

				fmt.Fprintf(out, `
result, err := srv.%s(r.Context(), params)
	if err!=nil {
		if apierror, ok := err.(ApiError); ok {
			w.WriteHeader(apierror.HTTPStatus)
		} else {
			w.WriteHeader(500)
		}
		body := Body{
				Error: err.Error(),
			}
		bodyBytes, _:= json.Marshal(body)
		w.Write(bodyBytes)
		return
	}
	body := Body{
		Response: result,
	}
	bodyBytes, _ := json.Marshal(body)
	w.Write(bodyBytes)
`, fun.Name)
				fmt.Fprint(out, "}\n")
			}
		}

	}
	//generating ServeHttp
	for receiver, hs := range handlers {
		fmt.Fprintf(out, `
func (h *%s ) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
`, receiver)
		for _, h := range hs {
			fmt.Fprintf(out, `
case "%s":
		h.%s(w, r)
`, h.apigenInfo.URL, h.hanlderMethod)
		}

		fmt.Fprint(out, `
default:
		w.WriteHeader(404)
		body := Body{
				Error: "unknown method",
			}
		bodyBytes, _:= json.Marshal(body)
		w.Write(bodyBytes)
		return
	}
}
`)
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

func CreateBindFunction(out io.Writer, structName string, fields []*ast.Field) {
	fmt.Fprintf(out, "// Generating Bind Function for struct %s\n", structName)
	fmt.Fprintf(out, "func (model *%s) Bind(r *http.Request) error {\n", structName)

	for _, field := range fields {

		if field.Tag != nil {

			tag := reflect.StructTag(field.Tag.Value[1 : len(field.Tag.Value)-1])
			validatorTag, ok := tag.Lookup("apivalidator")
			if ok {
				vInfo := getValidationInfo(validatorTag)
				paramName := strings.ToLower(field.Names[0].Name)
				if vInfo.replaceParamName {
					paramName = vInfo.paramname
				}

				switch field.Type.(*ast.Ident).Name {
				case "string":
					fmt.Fprintf(out, "model.%s = r.FormValue(\"%s\") \n", field.Names[0].Name, paramName)
					ValidateStringField(out, field.Names[0].Name, paramName, vInfo)
					if vInfo.useDefault {
						fmt.Fprintf(out, `if model.%s == "" {
			model.%s = "%s"
		}
`, field.Names[0].Name, field.Names[0].Name, vInfo.defaultValue)
					}
				case "int":
					fmt.Fprintf(out, `
val, err := strconv.Atoi(r.FormValue("%s"))
if err != nil {
	return fmt.Errorf("%s must be int")
}
model.%s = val
 `, paramName, paramName, field.Names[0].Name)

					ValidateIntField(out, field.Names[0].Name, paramName, vInfo)
				}

			}

		}

	}
	fmt.Fprintln(out, "return nil")
	fmt.Fprint(out, "}\n")

}

func ValidateStringField(out io.Writer, field string, paramName string, vInfo validationInfo) {
	if vInfo.required {
		fmt.Fprintf(out, `
if model.%s == "" {
		return fmt.Errorf("%s must me not empty")
	}
`, field, paramName)
	}

	if vInfo.hasMax {
		fmt.Fprintf(out, `
if len(model.%s) > %d {
		return fmt.Errorf("%s len must be >= %d")
	}
`, field, vInfo.max, paramName, vInfo.max)
	}

	if vInfo.hasMin {
		fmt.Fprintf(out, `
if len(model.%s) < %d {
		return fmt.Errorf("%s len must be >= %d")
	}
`, field, vInfo.min, paramName, vInfo.min)
	}

	if len(vInfo.enumValues) > 0 {
		fmt.Fprintf(out, "switch model.%s {\n", field)
		for _, enumValue := range vInfo.enumValues {
			fmt.Fprintf(out, "case \"%s\":\n", enumValue)
		}
		if vInfo.useDefault {
			fmt.Fprint(out, "case \"\":\n")
		}
		fmt.Fprintf(out, `default:
		return fmt.Errorf("%s must be one of [%s]")
	}
`, paramName, strings.Join(vInfo.enumValues, ", "))
	}

}

func ValidateIntField(out io.Writer, field string, paramName string, vInfo validationInfo) {
	if vInfo.required {
		fmt.Fprintf(out, `
if model.%s == 0 {
		return fmt.Errorf("Field %s is required")
	}
`, field, paramName)
	}
	if vInfo.hasMax {
		fmt.Fprintf(out, `
if model.%s > %d {
		return fmt.Errorf("%s must be <= %d")
	}
`, field, vInfo.max, paramName, vInfo.max)
	}

	if vInfo.hasMin {
		fmt.Fprintf(out, `
if model.%s < %d {
		return fmt.Errorf("%s must be >= %d")
	}
`, field, vInfo.min, paramName, vInfo.min)
	}

}
