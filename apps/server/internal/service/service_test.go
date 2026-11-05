package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"recipeworkbench/apps/server/internal/apperr"
	"recipeworkbench/apps/server/internal/machine"
)

type stubReader struct {
	machines []machine.RecipeFile
	recipes  []machine.RecipeFile
	content  []byte
	err      error
}

func (s stubReader) Machines(context.Context) ([]machine.RecipeFile, error) {
	return s.machines, s.err
}

func (s stubReader) Recipes(context.Context, string) ([]machine.RecipeFile, error) {
	return s.recipes, s.err
}

func (s stubReader) Content(context.Context, string, string) ([]byte, error) {
	return s.content, s.err
}

func TestCatalogUseCasesDelegate(t *testing.T) {
	svc := New(stubReader{
		machines: []machine.RecipeFile{{ID: "M1", Name: "M1", Format: "pgs"}},
		recipes:  []machine.RecipeFile{{ID: "a.pgs", Name: "a.pgs", Format: "pgs"}},
	}, nil)
	machines, err := svc.Machines(context.Background())
	if err != nil || len(machines) != 1 || machines[0].ID != "M1" {
		t.Fatalf("machines: %+v %v", machines, err)
	}
	recipes, err := svc.Recipes(context.Background(), "M1")
	if err != nil || len(recipes) != 1 || recipes[0].Name != "a.pgs" {
		t.Fatalf("recipes: %+v %v", recipes, err)
	}
}

func TestErrorsFromTheReaderArePassedThrough(t *testing.T) {
	want := apperr.New(apperr.NotFound, "机台或配方不存在", nil)
	svc := New(stubReader{err: want}, nil)
	if _, err := svc.Machines(context.Background()); !errors.Is(err, want) {
		t.Fatalf("machines swallowed the cause: %v", err)
	}
	if _, err := svc.Recipes(context.Background(), "M1"); !errors.Is(err, want) {
		t.Fatalf("recipes swallowed the cause: %v", err)
	}
	if _, err := svc.Recipe(context.Background(), "M1", "a.pgs"); !errors.Is(err, want) {
		t.Fatalf("recipe swallowed the cause: %v", err)
	}
	if _, err := svc.Source(context.Background(), "M1", "a.pgs"); !errors.Is(err, want) {
		t.Fatalf("source swallowed the cause: %v", err)
	}
}

func TestRecipeParsesEverySampleFile(t *testing.T) {
	paths, err := filepath.Glob("../../../mockserver/testdata/*/*")
	if err != nil || len(paths) == 0 {
		t.Fatalf("missing sample data: %d %v", len(paths), err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		machineID, name := filepath.Base(filepath.Dir(path)), filepath.Base(path)
		svc := New(stubReader{content: data}, nil)
		body, err := svc.Recipe(context.Background(), machineID, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if body.MachineID != machineID || body.SourceName != name || len(body.Tests) == 0 || len(body.SHA256) != 64 {
			t.Fatalf("%s: broken contract %+v", name, body)
		}
	}
}

func TestMalformedRecipeIsAParseError(t *testing.T) {
	svc := New(stubReader{content: []byte("damaged recipe")}, nil)
	_, err := svc.Recipe(context.Background(), "M1", "a.pgs")
	if kind, ok := apperr.KindOf(err); !ok || kind != apperr.Parse {
		t.Fatalf("expected parse error, got %v", err)
	}
	if message := apperr.MessageOf(err); !strings.HasPrefix(message, "配方解析失败") {
		t.Fatalf("message lost detail: %s", message)
	}
}

func TestSourceReturnsRawBytes(t *testing.T) {
	svc := New(stubReader{content: []byte("raw")}, nil)
	data, err := svc.Source(context.Background(), "M1", "a.pgs")
	if err != nil || string(data) != "raw" {
		t.Fatalf("source: %q %v", data, err)
	}
}
