package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBinaryEmbedsTimezoneData(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list dependencies: %v", err)
	}

	for dependency := range strings.Lines(string(output)) {
		if strings.TrimSpace(dependency) == "time/tzdata" {
			return
		}
	}
	t.Fatal("time/tzdata is not linked into the metabol binary")
}
