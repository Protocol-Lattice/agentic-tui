package src

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExtractJSONFenced(t *testing.T) {
	input := "```json\n{\"key\": \"value\"}\n```"
	data, err := extractJSON(input)
	if err != nil {
		t.Fatalf("extractJSON returned error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}

	want := map[string]string{"key": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected map: got %v want %v", got, want)
	}
}

func TestExtractJSONBackticksAndTrailingComma(t *testing.T) {
	input := "Here you go:\n[{`name`: `server`, `path`: `src/server.go`, `lang`: `Go`, `goal`: `serve`,},]\n"
	data, err := extractJSON(input)
	if err != nil {
		t.Fatalf("extractJSON returned error: %v", err)
	}

	var got []planFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}

	want := []planFile{{
		Name: "server",
		Path: "src/server.go",
		Lang: "Go",
		Goal: "serve",
	}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected plan: got %#v want %#v", got, want)
	}
}

func TestExtractJSONNoJSON(t *testing.T) {
	if _, err := extractJSON("no structured data here"); err == nil {
		t.Fatalf("expected error when no JSON present")
	}
}
