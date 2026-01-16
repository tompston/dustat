package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
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
	var fix bool
	var dryRun bool
	flag.StringVar(&ignoreCsv, "ignore", "", "comma-separated list of exported identifiers to ignore")
	flag.BoolVar(&jsonOutput, "json", false, "output results in JSON format")
	flag.BoolVar(&fix, "fix", false, "automatically rename unused exported symbols to unexported")
	flag.BoolVar(&dryRun, "dry-run", false, "preview changes without applying them (requires --fix)")
	flag.Parse()

	if flag.NArg() < 1 {
		return fmt.Errorf("usage: dustat [--ignore=MyFunc,MyStruct] [--json] [--fix] [--dry-run] <path-to-project>")
	}

	if dryRun && !fix {
		return fmt.Errorf("--dry-run requires --fix")
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

	if err := reg.Run(!fix, jsonOutput); err != nil {
		return err
	}

	if fix {
		return reg.Fix(dryRun)
	}

	return nil
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

// Fix renames all unused exported symbols to unexported using gopls
func (reg *Registry) Fix(dryRun bool) error {
	// Check if gopls is installed
	if _, err := exec.LookPath("gopls"); err != nil {
		return fmt.Errorf("gopls not found. Install with: go install golang.org/x/tools/gopls@latest")
	}

	if len(reg.Result) == 0 {
		fmt.Println("No unused exported symbols to fix!")
		return nil
	}

	// Sort by file and line to process in order
	sort.Slice(reg.Result, func(i, j int) bool {
		if reg.Result[i].Pos.Filename != reg.Result[j].Pos.Filename {
			return reg.Result[i].Pos.Filename < reg.Result[j].Pos.Filename
		}
		return reg.Result[i].Pos.Line < reg.Result[j].Pos.Line
	})

	successful := 0
	skipped := 0
	failed := 0

	for _, decl := range reg.Result {
		newName := toUnexported(decl.Name)

		// Skip if name doesn't change (shouldn't happen with exported symbols)
		if newName == decl.Name {
			if dryRun {
				fmt.Printf("⊘ Skip: %s (already unexported) at %s\n", decl.Name, decl.Pos.String())
			}
			skipped++
			continue
		}

		if dryRun {
			fmt.Printf("→ Would rename: %s -> %s at %s\n", decl.Name, newName, decl.Pos.String())
			successful++
			continue
		}

		// Use gopls to rename
		position := fmt.Sprintf("%s:%d:%d", decl.Pos.Filename, decl.Pos.Line, decl.Pos.Column)

		cmd := exec.Command("gopls", "rename", "-w", position, newName)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ Failed to rename %s: %v\n  %s\n", decl.Name, err, stderr.String())
			failed++
			continue
		}

		fmt.Printf("✓ Renamed: %s -> %s\n", decl.Name, newName)
		successful++
	}

	fmt.Println()
	if dryRun {
		fmt.Printf("Dry-run summary: %d would be renamed, %d skipped\n", successful, skipped)
	} else {
		fmt.Printf("Summary: %d renamed, %d skipped, %d failed\n", successful, skipped, failed)
		if failed > 0 {
			return fmt.Errorf("some renames failed")
		}
	}

	return nil
}

// toUnexported converts an exported identifier to unexported following Go naming conventions.
// It properly handles acronyms and initialisms:
// - HTTPServer -> httpServer (not hTTPServer)
// - XMLParser -> xmlParser (not xMLParser)
// - APIHandler -> apiHandler (not aPIHandler)
// - ID -> id (not iD)
// - URLPath -> urlPath
// - HTTPs -> https (not httPs)
func toUnexported(name string) string {
	if name == "" {
		return name
	}

	runes := []rune(name)
	if len(runes) == 1 {
		return string(unicode.ToLower(runes[0]))
	}

	// Find the end of the leading uppercase sequence
	// This tells us where the first non-uppercase character is
	firstLower := -1
	for i := 0; i < len(runes); i++ {
		if !unicode.IsUpper(runes[i]) {
			firstLower = i
			break
		}
	}

	// If the whole name is uppercase, lowercase it all
	// Example: "HTTP" -> "http", "ID" -> "id"
	if firstLower == -1 {
		for i := range runes {
			runes[i] = unicode.ToLower(runes[i])
		}
		return string(runes)
	}

	// Now we have some uppercase letters followed by at least one non-uppercase
	// Determine how many leading characters to lowercase
	toLowerCount := firstLower

	// If we have 2+ uppercase letters followed by an uppercase letter (new word),
	// the last uppercase before the new word should not be lowercased
	// Example: "HTTPServer" -> firstLower=4 (at 'e'), runes[4-1]='S' is uppercase
	//          We want "http" + "Server", so lowercase first 4 chars (indices 0-3)
	// Example: "HTTPs" -> firstLower=4 (at 's'), runes[4-1]='P' is uppercase
	//          We want "https", so lowercase first 4 chars (indices 0-3)
	// Example: "MyFunc" -> firstLower=1 (at 'y'), runes[0]='M'
	//          We want "myFunc", so lowercase first 1 char

	if firstLower > 1 && unicode.IsUpper(runes[firstLower-1]) {
		// We have multiple uppercase letters before a non-uppercase
		// Check if the non-uppercase character is followed by more content
		// and if runes[firstLower-1] starts a new capitalized word

		// Pattern: "HTTPServer" where 'S' at firstLower-1 is start of "Server"
		// We want to keep 'S' capitalized, so only lowercase up to 'P'
		if firstLower < len(runes)-1 {
			// There are more characters after firstLower
			// The uppercase at firstLower-1 likely starts a new word
			toLowerCount = firstLower - 1
		}
		// else: Pattern like "HTTPs" - lowercase all the uppercase part
	}

	// Lowercase the determined portion
	for i := 0; i < toLowerCount; i++ {
		runes[i] = unicode.ToLower(runes[i])
	}

	return string(runes)
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
