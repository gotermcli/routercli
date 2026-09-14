// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestCaptureOutputReturnsPrintedLines - This test verifies the ordinary
// case: everything fn prints to os.Stdout comes back as individual lines,
// in order, with the trailing newline dropped from each one rather than
// producing a spurious empty final line.
func TestCaptureOutputReturnsPrintedLines(t *testing.T) {
	lines, err := CaptureOutput(func() {
		fmt.Println("line one")
		fmt.Println("line two")
		fmt.Println("line three")
	})
	if err != nil {
		t.Fatalf("CaptureOutput returned unexpected error: %v", err)
	}
	want := []string{"line one", "line two", "line three"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %v, want %v", lines, want)
	}
}

// TestCaptureOutputRestoresRealStdout - This test verifies that os.Stdout
// is restored once CaptureOutput returns, not left pointing at the now
// closed pipe, by printing again afterward and confirming that second
// print reaches this test's captured stdout rather than erroring out or
// vanishing.
func TestCaptureOutputRestoresRealStdout(t *testing.T) {
	before := os.Stdout

	if _, err := CaptureOutput(func() { fmt.Println("inside") }); err != nil {
		t.Fatalf("CaptureOutput returned unexpected error: %v", err)
	}

	if os.Stdout != before {
		t.Error("os.Stdout was not restored to its original value after CaptureOutput returned")
	}

	// A real, uncaptured write to the restored os.Stdout must still
	// succeed, confirming the pipe's write end was not left dangling in
	// os.Stdout's place.
	if _, err := fmt.Fprintln(os.Stdout, "after restore"); err != nil {
		t.Errorf("write to os.Stdout after CaptureOutput returned an error: %v", err)
	}
}

// TestCaptureOutputEmptyOutputReturnsNilLines - This test verifies that a
// handler which prints nothing at all returns a nil, not just empty, lines
// slice, and no error.
func TestCaptureOutputEmptyOutputReturnsNilLines(t *testing.T) {
	lines, err := CaptureOutput(func() {})
	if err != nil {
		t.Fatalf("CaptureOutput returned unexpected error: %v", err)
	}
	if lines != nil {
		t.Errorf("lines = %v, want nil", lines)
	}
}

// TestCaptureOutputHandlesALineLongerThanTheDefaultScannerBuffer - This
// test verifies the reason CaptureOutput grows bufio.Scanner's buffer well
// past its default 64 KiB limit: a single line longer than that default,
// plausible for a very large "show running-config" on a real device, must
// come back intact, not truncated or dropped with a scanner error.
func TestCaptureOutputHandlesALineLongerThanTheDefaultScannerBuffer(t *testing.T) {
	long := strings.Repeat("x", 200*1024)

	lines, err := CaptureOutput(func() { fmt.Println(long) })
	if err != nil {
		t.Fatalf("CaptureOutput returned unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0] != long {
		t.Errorf("got %d line(s) of length %d, want exactly one line of length %d", len(lines), len(firstOrEmpty(lines)), len(long))
	}
}

// firstOrEmpty - This helper returns lines[0], or "" when lines is empty,
// purely so
// TestCaptureOutputHandlesALineLongerThanTheDefaultScannerBuffer's failure
// message can report a length without a second, separate bounds check
// cluttering that test itself.
func firstOrEmpty(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// TestCaptureOutputPanicInsideFnStillRestoresStdout - This test verifies
// that a panic inside fn does not leave os.Stdout pointing at a broken
// pipe forever, since CaptureOutput restores it through a deferred
// assignment that runs regardless of how fn itself returns. The panic is
// expected to propagate out of CaptureOutput, matching how any other panic
// inside a command's RunFunc would already propagate with no special
// handling from this project at all; this test only cares that stdout is
// left usable afterward.
func TestCaptureOutputPanicInsideFnStillRestoresStdout(t *testing.T) {
	before := os.Stdout

	func() {
		defer func() { recover() }()
		CaptureOutput(func() { panic("boom") })
	}()

	if os.Stdout != before {
		t.Error("os.Stdout was not restored after a panic inside fn")
	}
}
