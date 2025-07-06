package main

import (
	"fmt"
	"testing"
)

func TestFlow(t *testing.T) {
	const testProjectPath = "./test"

	t.Run("all-unused-found", func(t *testing.T) {
		reg, err := NewRegistry(testProjectPath)
		if err != nil {
			t.Fatalf("failed to create registry: %v", err)
		}

		if err := reg.Run(false); err != nil {
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

		if err := reg.Run(true); err != nil {
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
