package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := runFromCli(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runFromCli() error {
	var ignoreCsv string
	var jsonOutput bool
	flag.StringVar(&ignoreCsv, "ignore", "", "comma-separated list of exported identifiers to ignore")
	flag.BoolVar(&jsonOutput, "json", false, "output results in JSON format")
	flag.Parse()

	if flag.NArg() < 1 {
		return fmt.Errorf("usage: dustat [--ignore=MyFunc,MyStruct] [--json] <path-to-project>")
	}

	projectPath, err := getProjectPath(flag.Arg(0))
	if err != nil {
		return fmt.Errorf("error getting project path: %v", err)
	}

	reg, err := NewRegistry(projectPath)
	if err != nil {
		return fmt.Errorf("error creating registry: %v", err)
	}

	ignore := make(map[string]struct{})
	if ignoreCsv != "" {
		for _, name := range strings.Split(ignoreCsv, ",") {
			ignore[strings.TrimSpace(name)] = struct{}{}
		}

		reg.WithIgnoreList(ignore)
	}

	return reg.Run(true, jsonOutput)
}

type Decl struct {
	Name      string
	Pos       token.Position
	End       token.Position
	LineCount int
}

type Registry struct {
	Path           string              // Path is the root path of the project being analyzed
	Ignore         map[string]struct{} // Identifiers that should be ignored in the analysis
	Declarations   map[string]Decl     // Declarations holds all exported identifiers found in the project
	UsageCount     map[string]int      // UsageCount tracks how many times each identifier is used
	Result         []Decl              // Result holds the final unused declarations
	TotalUnusedLoc int                 // TotalUnusedLoc counts the total number of unused lines across all unused declarations
}

func NewRegistry(path string) (*Registry, error) {
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("path does not exist: %s", path)
	}

	return &Registry{
		Declarations: make(map[string]Decl),
		UsageCount:   make(map[string]int),
		Ignore:       make(map[string]struct{}),
		Result:       []Decl{},
		Path:         path,
	}, nil
}

func (reg *Registry) WithIgnoreList(ignore map[string]struct{}) *Registry {
	if ignore == nil {
		ignore = make(map[string]struct{})
	}

	reg.Ignore = ignore
	return reg
}

func (reg *Registry) Run(printResult bool, jsonOutput bool) error {
	if err := reg.ParseFiles(); err != nil {
		return fmt.Errorf("error parsing project: %v", err)
	}

	if err := reg.AccumulateResult(); err != nil {
		return fmt.Errorf("error accumulating results: %v", err)
	}

	if printResult {
		reg.Report(jsonOutput)
	}

	return nil
}

func (reg *Registry) ParseFiles() error {

	fset := token.NewFileSet()
	projectPath := reg.Path

	// First pass: collect declarations and usage from non-test files
	if err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && (info.Name() == "vendor" || info.Name() == "testdata" || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}

		if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") {
			return nil // skip test files for this pass
		}

		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return fmt.Errorf("error parsing file %s: %v", path, err)
		}

		// Collect exported declarations
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() {
					reg.Declarations[d.Name.Name] = makeDecl(d.Name.Name, d.Name.Pos(), d.End(), fset)
				}

			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {

							reg.Declarations[s.Name.Name] = makeDecl(s.Name.Name, s.Name.Pos(), s.End(), fset)
						}

					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.IsExported() {
								reg.Declarations[name.Name] = makeDecl(name.Name, name.Pos(), s.End(), fset)
							}
						}
					}
				}
			}
		}

		// Collect usage
		ast.Inspect(file, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				reg.UsageCount[ident.Name]++
			}
			return true
		})

		return nil
	}); err != nil {
		return fmt.Errorf("error walking project: %v", err)
	}

	// Second pass: collect usage from _test.go files
	if err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			return fmt.Errorf("error parsing test file %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				reg.UsageCount[ident.Name]++
			}
			return true
		})

		return nil
	}); err != nil {
		return fmt.Errorf("error walking test files: %v", err)
	}

	return nil
}

func (reg *Registry) AccumulateResult() error {
	for name, decl := range reg.Declarations {
		if _, ignore := reg.Ignore[name]; ignore {
			continue
		}

		if reg.UsageCount[name] <= 1 {
			reg.Result = append(reg.Result, decl)
			reg.TotalUnusedLoc += decl.LineCount
		}
	}

	return nil
}

type Issue struct {
	Symbol string `json:"symbol"`
	Line   int    `json:"line"`
}

type FileIssues struct {
	File   string  `json:"file"`
	Issues []Issue `json:"issues"`
}

func (reg *Registry) Report(jsonOutput bool) {
	if jsonOutput {
		reg.ReportJSON()
		return
	}

	if len(reg.Result) > 0 {

		// sort ascending by the number of lines in the declaration
		sort.Slice(reg.Result, func(i, j int) bool {
			return reg.Result[i].LineCount < reg.Result[j].LineCount
		})

		fmt.Printf("Unused Exported Symbols (ignoring test-only usage):\n")
		fmt.Println("========================================================")

		for _, decl := range reg.Result {
			fmt.Printf("%-5v %s (%v)\n", decl.LineCount, decl.Name, decl.Pos.String())
		}

		fmt.Println("========================================================")
		fmt.Printf("Total Unused Lines: %d, Declarations: %v\n", reg.TotalUnusedLoc, len(reg.Result))

	} else {
		fmt.Println("No unused exported identifiers found!")
	}
}

func (reg *Registry) ReportJSON() {
	// Group issues by file
	fileMap := make(map[string][]Issue)
	for _, decl := range reg.Result {
		filePath := decl.Pos.Filename
		fileMap[filePath] = append(fileMap[filePath], Issue{
			Symbol: decl.Name,
			Line:   decl.Pos.Line,
		})
	}

	// Convert map to slice and sort by file path
	results := []FileIssues{}
	for file, issues := range fileMap {
		// Sort issues by line number within each file
		sort.Slice(issues, func(i, j int) bool {
			return issues[i].Line < issues[j].Line
		})
		results = append(results, FileIssues{
			File:   file,
			Issues: issues,
		})
	}

	// Sort by file path
	sort.Slice(results, func(i, j int) bool {
		return results[i].File < results[j].File
	})

	// Output JSON
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling JSON: %v\n", err)
		return
	}
	fmt.Println(string(output))
}

func makeDecl(name string, start, end token.Pos, fset *token.FileSet) Decl {
	pos := fset.Position(start)
	endPos := fset.Position(end)
	return Decl{
		LineCount: endPos.Line - pos.Line + 1,
		End:       endPos,
		Pos:       pos,
		Name:      name,
	}
}

func getProjectPath(cliPath string) (string, error) {
	if cliPath == "" {
		return "", fmt.Errorf("no project path provided")
	}

	if cliPath == "." {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("unable to determine current working directory: %v", err)
		}
		return cwd, nil
	}

	absPath, err := filepath.Abs(cliPath)
	if err != nil {
		return "", fmt.Errorf("unable to resolve absolute path: %v", err)
	}

	return absPath, nil
}
