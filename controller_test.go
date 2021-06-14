package main

import (
	"fmt"
	"testing"
)

func TestIsValidUserInput(t *testing.T) {
	var tests = []struct {
		input string
		want  bool
	}{
		{"this-is-a-valid-resource-name", true},
		{"hello@world", false},
		{"THIS-IS-NOT-VALID", false},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%s,%t", tt.input, tt.want)
		t.Run(testname, func(t *testing.T) {
			ans := isValidUserInput(tt.input)
			if ans != tt.want {
				t.Errorf("got %t, want %t", ans, tt.want)
			}
		})
	}
}
