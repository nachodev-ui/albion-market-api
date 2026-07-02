package contracts

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

type openAPIInventory struct {
	Version string
	Routes  map[contractRoute]string
	Schemas map[string][]string
}

type contractRoute struct {
	Method string
	Path   string
}

func TestOpenAPIPathsMatchRegisteredGoHandlers(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	inventory := loadOpenAPIInventory(t, root)
	goRoutes := routesFromGoSources(t, root)

	if diff := routeSetDiff(goRoutes, inventory.Routes); diff != "" {
		t.Fatalf("OpenAPI and Go route inventories diverged:\n%s", diff)
	}
}

func TestOpenAPISchemaPropertiesMatchGoJSONTags(t *testing.T) {
	t.Parallel()

	inventory := loadOpenAPIInventory(t, repositoryRoot(t))
	contracts := map[string]reflect.Type{
		"MarketDefinition":          reflect.TypeOf(domain.MarketDefinition{}),
		"MarketCatalogResponse":     reflect.TypeOf(domain.MarketCatalogResponse{}),
		"PriceQueryEntry":           reflect.TypeOf(domain.PriceQueryEntry{}),
		"PriceQueryRequest":         reflect.TypeOf(domain.PriceQueryRequest{}),
		"CurrentPrice":              reflect.TypeOf(domain.CurrentPrice{}),
		"PriceQueryResponse":        reflect.TypeOf(domain.PriceQueryResponse{}),
		"HistoryQueryEntry":         reflect.TypeOf(domain.HistoryQueryEntry{}),
		"HistoryQueryRequest":       reflect.TypeOf(domain.HistoryQueryRequest{}),
		"MarketHistoryPoint":        reflect.TypeOf(domain.MarketHistoryPoint{}),
		"MarketHistorySeries":       reflect.TypeOf(domain.MarketHistorySeries{}),
		"HistoryQueryResponse":      reflect.TypeOf(domain.HistoryQueryResponse{}),
		"PriceIngest":               reflect.TypeOf(domain.PriceIngest{}),
		"IngestPricesRequest":       reflect.TypeOf(domain.IngestPricesRequest{}),
		"IngestPricesResponseBase":  reflect.TypeOf(domain.IngestPricesResponse{}),
		"HistoryBucketIngest":       reflect.TypeOf(domain.HistoryBucketIngest{}),
		"HistoryIngest":             reflect.TypeOf(domain.HistoryIngest{}),
		"IngestHistoryRequest":      reflect.TypeOf(domain.IngestHistoryRequest{}),
		"IngestHistoryResponseBase": reflect.TypeOf(domain.IngestHistoryResponse{}),
	}

	var mismatches []string
	for schemaName, goType := range contracts {
		openAPIProperties, ok := inventory.Schemas[schemaName]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("missing OpenAPI schema %q for Go type %s", schemaName, goType.Name()))
			continue
		}

		goProperties := jsonPropertyNames(t, goType)
		if !reflect.DeepEqual(goProperties, openAPIProperties) {
			mismatches = append(mismatches, fmt.Sprintf(
				"schema %s != Go type %s\n  OpenAPI: %v\n  Go JSON: %v",
				schemaName,
				goType.Name(),
				openAPIProperties,
				goProperties,
			))
		}
	}

	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		t.Fatalf("OpenAPI schema properties and Go JSON tags diverged:\n%s", strings.Join(mismatches, "\n"))
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate contract test source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func loadOpenAPIInventory(t *testing.T, root string) openAPIInventory {
	t.Helper()
	filename := filepath.Join(root, "openapi", "openapi.yaml")
	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("open OpenAPI contract: %v", err)
	}
	defer file.Close()

	inventory := openAPIInventory{
		Routes:  make(map[contractRoute]string),
		Schemas: make(map[string][]string),
	}
	operations := map[string]struct{}{
		"get": {}, "post": {}, "put": {}, "patch": {}, "delete": {}, "head": {}, "options": {}, "trace": {},
	}

	section := ""
	inSchemas := false
	inProperties := false
	currentPath := ""
	currentRoute := contractRoute{}
	currentSchema := ""

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := scanner.Text()
		if strings.ContainsRune(raw, '\t') {
			t.Fatalf("%s:%d: tabs are not supported in the canonical OpenAPI layout", filename, lineNumber)
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))

		if indent == 0 {
			section = ""
			inSchemas = false
			inProperties = false
			currentPath = ""
			currentRoute = contractRoute{}
			currentSchema = ""

			key, value, ok := splitYAMLKeyValue(trimmed)
			if !ok {
				continue
			}
			switch key {
			case "openapi":
				inventory.Version = value
			case "paths", "components":
				section = key
			}
			continue
		}

		switch section {
		case "paths":
			if indent == 2 {
				key, _, ok := splitYAMLKeyValue(trimmed)
				if ok && strings.HasPrefix(key, "/") {
					currentPath = key
					currentRoute = contractRoute{}
				}
				continue
			}
			if indent == 4 && currentPath != "" {
				key, _, ok := splitYAMLKeyValue(trimmed)
				method := strings.ToLower(key)
				if ok {
					if _, supported := operations[method]; supported {
						currentRoute = contractRoute{Method: strings.ToUpper(method), Path: currentPath}
						if _, exists := inventory.Routes[currentRoute]; exists {
							t.Fatalf("%s:%d: duplicate OpenAPI operation %s %s", filename, lineNumber, currentRoute.Method, currentRoute.Path)
						}
						inventory.Routes[currentRoute] = ""
					}
				}
				continue
			}
			if indent == 6 && currentRoute.Path != "" {
				key, value, ok := splitYAMLKeyValue(trimmed)
				if ok && key == "operationId" {
					inventory.Routes[currentRoute] = value
				}
			}

		case "components":
			if indent == 2 {
				key, _, ok := splitYAMLKeyValue(trimmed)
				inSchemas = ok && key == "schemas"
				inProperties = false
				currentSchema = ""
				continue
			}
			if !inSchemas {
				continue
			}
			if indent == 4 {
				key, _, ok := splitYAMLKeyValue(trimmed)
				if ok {
					currentSchema = key
					inProperties = false
				}
				continue
			}
			if indent == 6 && currentSchema != "" {
				key, _, ok := splitYAMLKeyValue(trimmed)
				inProperties = ok && key == "properties"
				if inProperties {
					if _, exists := inventory.Schemas[currentSchema]; !exists {
						inventory.Schemas[currentSchema] = []string{}
					}
				}
				continue
			}
			if indent == 8 && inProperties && currentSchema != "" {
				key, _, ok := splitYAMLKeyValue(trimmed)
				if ok {
					inventory.Schemas[currentSchema] = append(inventory.Schemas[currentSchema], key)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan OpenAPI contract: %v", err)
	}
	if inventory.Version != "3.1.1" {
		t.Fatalf("OpenAPI version = %q, want 3.1.1", inventory.Version)
	}
	for schemaName := range inventory.Schemas {
		sort.Strings(inventory.Schemas[schemaName])
	}
	return inventory
}

func splitYAMLKeyValue(line string) (string, string, bool) {
	index := strings.IndexByte(line, ':')
	if index < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:index])
	value := strings.TrimSpace(line[index+1:])
	if key == "" {
		return "", "", false
	}
	if len(key) >= 2 {
		if key[0] == '"' && key[len(key)-1] == '"' {
			unquoted, err := strconv.Unquote(key)
			if err == nil {
				key = unquoted
			}
		} else if key[0] == '\'' && key[len(key)-1] == '\'' {
			key = strings.ReplaceAll(key[1:len(key)-1], "''", "'")
		}
	}
	return key, value, true
}

func routesFromGoSources(t *testing.T, root string) map[contractRoute]string {
	t.Helper()
	handlerMethods := handlerHTTPMethods(t, filepath.Join(root, "internal", "handlers"))

	filename := filepath.Join(root, "internal", "server", "router.go")
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}

	routes := make(map[contractRoute]string)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}

		pathLiteral, ok := call.Args[0].(*ast.BasicLit)
		if !ok || pathLiteral.Kind != token.STRING {
			t.Fatalf("router HandleFunc path must be a string literal at %s", fileSet.Position(call.Pos()))
		}
		path, err := strconv.Unquote(pathLiteral.Value)
		if err != nil {
			t.Fatalf("decode route path at %s: %v", fileSet.Position(pathLiteral.Pos()), err)
		}

		handlerSelector, ok := call.Args[1].(*ast.SelectorExpr)
		if !ok {
			t.Fatalf("router HandleFunc handler must be a selector at %s", fileSet.Position(call.Pos()))
		}
		handlerName := handlerSelector.Sel.Name
		method, ok := handlerMethods[handlerName]
		if !ok {
			t.Fatalf("cannot determine allowed HTTP method for handler %s registered at %s", handlerName, fileSet.Position(call.Pos()))
		}

		key := contractRoute{Method: method, Path: path}
		if previous, exists := routes[key]; exists {
			t.Fatalf("duplicate Go route %s %s (%s and %s)", method, path, previous, handlerName)
		}
		routes[key] = handlerName
		return true
	})
	return routes
}

func handlerHTTPMethods(t *testing.T, directory string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read handlers directory: %v", err)
	}

	methods := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		filename := filepath.Join(directory, entry.Name())
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filename, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}

		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Body == nil {
				continue
			}
			method := guardedHTTPMethod(function.Body)
			if method == "" {
				continue
			}
			if previous, exists := methods[function.Name.Name]; exists && previous != method {
				t.Fatalf("handler %s has conflicting method guards: %s and %s", function.Name.Name, previous, method)
			}
			methods[function.Name.Name] = method
		}
	}
	return methods
}

func guardedHTTPMethod(body *ast.BlockStmt) string {
	method := ""
	ast.Inspect(body, func(node ast.Node) bool {
		if method != "" {
			return false
		}
		expression, ok := node.(*ast.BinaryExpr)
		if !ok || expression.Op != token.NEQ {
			return true
		}

		if isRequestMethod(expression.X) {
			method = httpMethodConstant(expression.Y)
		} else if isRequestMethod(expression.Y) {
			method = httpMethodConstant(expression.X)
		}
		return method == ""
	})
	return method
}

func isRequestMethod(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Method"
}

func httpMethodConstant(expression ast.Expr) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	identifier, ok := selector.X.(*ast.Ident)
	if !ok || identifier.Name != "http" || !strings.HasPrefix(selector.Sel.Name, "Method") {
		return ""
	}
	return strings.ToUpper(strings.TrimPrefix(selector.Sel.Name, "Method"))
}

func routeSetDiff(goRoutes, openAPIRoutes map[contractRoute]string) string {
	var lines []string
	for route, handler := range goRoutes {
		if _, ok := openAPIRoutes[route]; !ok {
			lines = append(lines, fmt.Sprintf("missing from OpenAPI: %s %s (Go handler %s)", route.Method, route.Path, handler))
		}
	}
	for route, operationID := range openAPIRoutes {
		if _, ok := goRoutes[route]; !ok {
			lines = append(lines, fmt.Sprintf("missing from Go router: %s %s (OpenAPI operationId %s)", route.Method, route.Path, operationID))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func jsonPropertyNames(t *testing.T, goType reflect.Type) []string {
	t.Helper()
	properties := make([]string, 0, goType.NumField())
	for index := 0; index < goType.NumField(); index++ {
		field := goType.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			t.Fatalf("exported field %s.%s has no explicit json tag", goType.Name(), field.Name)
		}
		properties = append(properties, name)
	}
	sort.Strings(properties)
	return properties
}
