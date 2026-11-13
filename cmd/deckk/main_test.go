package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDoctorFailsOnABrokenConfigAndSaysWhere(t *testing.T) {
	var out bytes.Buffer
	if doctor(&out, nil, errors.New("line 3: bad value")) {
		t.Fatal("a config that doesn't read is a failure")
	}
	if !strings.Contains(out.String(), "✗ The config file doesn't read") || !strings.Contains(out.String(), "line 3: bad value") {
		t.Fatalf("output:\n%s", out.String())
	}
}
