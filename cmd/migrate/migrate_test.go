package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsMissingMode(t *testing.T) {
	err := run(context.Background(), nil, func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("error = %v, want usage error", err)
	}
}

func TestRunRejectsMissingDatabaseURL(t *testing.T) {
	err := run(context.Background(), []string{"status", "--format", "json"}, func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("error = %v, want DATABASE_URL error", err)
	}
}

func TestRedactDatabase(t *testing.T) {
	got := redactDatabase("postgres://user:pass@localhost:5432/commerce?sslmode=disable")
	if strings.Contains(got, "user") || strings.Contains(got, "pass") {
		t.Fatalf("redacted database leaked credentials: %s", got)
	}
	if !strings.Contains(got, "localhost:5432") {
		t.Fatalf("redacted database lost host details: %s", got)
	}
}
