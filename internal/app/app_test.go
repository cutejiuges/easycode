package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunHeadlessScaffold(t *testing.T) {
	var output bytes.Buffer
	err := Run(context.Background(), Options{Headless: true, Output: &output})
	if err != nil {
		t.Fatalf("run headless app: %v", err)
	}
	if !strings.Contains(output.String(), "scaffold is ready") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}
