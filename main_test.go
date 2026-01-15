package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"unicode"
)

func TestFlow(t *testing.T) {
	const testProjectPath = "./test"

	t.Run("all-unused-found", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		if err := reg.Run(false, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		fmt.Printf("reg.Result: %v\n", reg.Result)

		if len(reg.Result) == 0 {
			t.Fatal("expected unused declarations, but found none")
		}

		if err := resultIncludesName(reg.Result, "UnusedStruct"); err != nil {
			t.Errorf("expected unused declaration 'UnusedStruct' not found: %v", err)
		}
	})

	t.Run("igore-list-works", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		ignoreList := map[string]struct{}{
			"UnusedButIgnoredStruct": {},
		}

		reg.WithIgnoreList(ignoreList)

		if err := reg.Run(true, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		if err := resultIncludesName(reg.Result, "UnusedButIgnoredStruct"); err == nil {
			t.Fatal("expected ignored declaration 'UnusedButIgnoredStruct' to be excluded, but it was found")
		}

		if len(reg.Result) != 2 {
			t.Fatalf("expected 2 unused declarations, but found %d", len(reg.Result))
		}
	})
}

func resultIncludesName(result []Decl, name string) error {
	for _, decl := range result {
		if decl.Name == name {
			return nil
		}
	}

	return fmt.Errorf("expected name %v not found in result", name)
}

func TestToUnexported(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic cases
		{"simple", "MyFunc", "myFunc"},
		{"single char", "A", "a"},
		{"empty", "", ""},
		{"already lowercase", "myFunc", "myFunc"},

		// Common acronyms with words
		{"HTTP with word", "HTTPServer", "httpServer"},
		{"API with word", "APIHandler", "apiHandler"},
		{"XML with word", "XMLParser", "xmlParser"},
		{"URL with word", "URLPath", "urlPath"},
		{"JSON with word", "JSONEncoder", "jsonEncoder"},
		{"SQL with word", "SQLDatabase", "sqlDatabase"},
		{"TCP with word", "TCPConnection", "tcpConnection"},
		{"UDP with word", "UDPSocket", "udpSocket"},
		{"TLS with word", "TLSConfig", "tlsConfig"},
		{"DNS with word", "DNSResolver", "dnsResolver"},

		// ID cases (special case mentioned in requirements)
		{"ID alone", "ID", "id"},
		{"ID with prefix", "UserID", "userID"},
		{"ID with suffix", "IDGenerator", "idGenerator"},
		{"AppID", "AppID", "appID"},
		{"RequestID", "RequestID", "requestID"},

		// Multiple acronyms
		{"HTTP + API", "HTTPAPI", "httpapi"},
		{"XML + HTTP", "XMLHTTP", "xmlhttp"},
		{"HTTP + URL", "HTTPURL", "httpurl"},
		{"API + URL", "APIURL", "apiurl"},

		// Multiple acronyms with word
		{"XML + HTTP + Request", "XMLHTTPRequest", "xmlhttpRequest"},
		{"API + URL + Path", "APIURLPath", "apiurlPath"},

		// All caps (acronyms only)
		{"HTTP only", "HTTP", "http"},
		{"API only", "API", "api"},
		{"XML only", "XML", "xml"},
		{"URL only", "URL", "url"},
		{"JSON only", "JSON", "json"},

		// Edge cases
		{"two letters", "IO", "io"},
		{"acronym + single letter", "HTTPs", "https"},
		{"single uppercase + word", "AStruct", "aStruct"},

		// Real-world examples
		{"HTTPClient", "HTTPClient", "httpClient"},
		{"HTTPRequest", "HTTPRequest", "httpRequest"},
		{"HTTPResponse", "HTTPResponse", "httpResponse"},
		{"XMLDecoder", "XMLDecoder", "xmlDecoder"},
		{"JSONMarshaler", "JSONMarshaler", "jsonMarshaler"},
		{"URLEncoder", "URLEncoder", "urlEncoder"},
		{"APIClient", "APIClient", "apiClient"},
		{"APIKey", "APIKey", "apiKey"},
		{"DBConnection", "DBConnection", "dbConnection"},
		{"OSVersion", "OSVersion", "osVersion"},

		// Should NOT use mixed case
		{"avoid Url", "Url", "url"},    // Should be URL or url, not Url
		{"avoid Http", "Http", "http"}, // Should be HTTP or http, not Http
		{"avoid Id", "Id", "id"},       // Should be ID or id, not Id
		{"avoid Api", "Api", "api"},    // Should be API or api, not Api
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toUnexported(tt.input)
			if result != tt.expected {
				t.Errorf("toUnexported(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToUnexportedProperties(t *testing.T) {
	t.Run("result should start with lowercase", func(t *testing.T) {
		inputs := []string{"MyFunc", "HTTPServer", "ID", "APIKey", "XMLParser"}
		for _, input := range inputs {
			if input == "" {
				continue
			}
			result := toUnexported(input)
			if result != "" && !unicode.IsLower(rune(result[0])) {
				t.Errorf("toUnexported(%q) = %q; first character should be lowercase", input, result)
			}
		}
	})

	t.Run("should not create mixed-case acronyms", func(t *testing.T) {
		// These are invalid patterns we want to avoid
		badPatterns := []struct {
			input    string
			badStart string
		}{
			{"HTTPServer", "hTTP"}, // Should be "http", not "hTTP"
			{"APIHandler", "aPI"},  // Should be "api", not "aPI"
			{"XMLParser", "xML"},   // Should be "xml", not "xML"
			{"ID", "iD"},           // Should be "id", not "iD"
		}

		for _, bp := range badPatterns {
			result := toUnexported(bp.input)
			if len(result) >= len(bp.badStart) && result[:len(bp.badStart)] == bp.badStart {
				t.Errorf("toUnexported(%q) = %q; should not start with %q (mixed-case acronym)",
					bp.input, result, bp.badStart)
			}
		}
	})
}

// captureJSONOutput captures stdout and returns the JSON output as a string
func captureJSONOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to copy output: %v", err)
	}

	return buf.String()
}

func TestJSONOutput(t *testing.T) {
	const testProjectPath = "./test"

	t.Run("json-output-is-valid", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		if err := reg.Run(false, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		output := captureJSONOutput(t, func() {
			reg.ReportJSON()
		})

		// Verify output is valid JSON
		var result []FileIssues
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, output)
		}

		if len(result) == 0 {
			t.Fatal("expected at least one file with issues in JSON output")
		}
	})

	t.Run("json-output-contains-expected-fields", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		if err := reg.Run(false, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		output := captureJSONOutput(t, func() {
			reg.ReportJSON()
		})

		var result []FileIssues
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		// Verify structure
		if len(result) == 0 {
			t.Fatal("expected at least one file in result")
		}

		for _, fileIssues := range result {
			if fileIssues.File == "" {
				t.Error("expected file path to be non-empty")
			}

			if len(fileIssues.Issues) == 0 {
				t.Errorf("expected at least one issue for file %s", fileIssues.File)
			}

			for _, issue := range fileIssues.Issues {
				if issue.Symbol == "" {
					t.Errorf("expected symbol to be non-empty in file %s", fileIssues.File)
				}
				if issue.Line <= 0 {
					t.Errorf("expected line number to be positive, got %d for symbol %s", issue.Line, issue.Symbol)
				}
			}
		}
	})

	t.Run("json-output-includes-expected-symbols", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		if err := reg.Run(false, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		output := captureJSONOutput(t, func() {
			reg.ReportJSON()
		})

		var result []FileIssues
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		// Check that UnusedStruct is in the results
		foundUnusedStruct := false
		for _, fileIssues := range result {
			for _, issue := range fileIssues.Issues {
				if issue.Symbol == "UnusedStruct" {
					foundUnusedStruct = true
					if issue.Line != 12 {
						t.Errorf("expected UnusedStruct at line 12, got line %d", issue.Line)
					}
					break
				}
			}
		}

		if !foundUnusedStruct {
			t.Error("expected to find UnusedStruct in JSON output")
		}
	})

	t.Run("json-output-respects-ignore-list", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		ignoreList := map[string]struct{}{
			"UnusedButIgnoredStruct": {},
		}
		reg.WithIgnoreList(ignoreList)

		if err := reg.Run(false, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		output := captureJSONOutput(t, func() {
			reg.ReportJSON()
		})

		var result []FileIssues
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		// Verify UnusedButIgnoredStruct is not in results
		for _, fileIssues := range result {
			for _, issue := range fileIssues.Issues {
				if issue.Symbol == "UnusedButIgnoredStruct" {
					t.Error("expected UnusedButIgnoredStruct to be ignored, but found in JSON output")
				}
			}
		}
	})

	t.Run("json-output-empty-when-all-ignored", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		ignoreList := map[string]struct{}{
			"UnusedStruct":           {},
			"UnusedButIgnoredStruct": {},
			"MY_CONS":                {},
		}
		reg.WithIgnoreList(ignoreList)

		if err := reg.Run(false, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		output := captureJSONOutput(t, func() {
			reg.ReportJSON()
		})

		var result []FileIssues
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if len(result) != 0 {
			t.Errorf("expected empty array, got %d files", len(result))
		}

		// Verify output is literally "[]"
		expectedOutput := "[]"
		if output[:len(expectedOutput)] != expectedOutput {
			t.Errorf("expected output to start with '[]', got: %s", output)
		}
	})

	t.Run("json-issues-sorted-by-line", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		if err := reg.Run(false, false); err != nil {
			t.Fatalf("failed to run registry: %v", err)
		}

		output := captureJSONOutput(t, func() {
			reg.ReportJSON()
		})

		var result []FileIssues
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		// Verify issues are sorted by line number within each file
		for _, fileIssues := range result {
			for i := 1; i < len(fileIssues.Issues); i++ {
				if fileIssues.Issues[i].Line < fileIssues.Issues[i-1].Line {
					t.Errorf("issues not sorted by line number in file %s: %d came before %d",
						fileIssues.File, fileIssues.Issues[i-1].Line, fileIssues.Issues[i].Line)
				}
			}
		}
	})
}
