// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package paging

import (
	"bufio"
	"bytes"
	"io"
	"os"
)

// CaptureOutput - Paging and output filtering both need the whole of a
// command's output before any of it reaches the terminal. Handlers print
// straight to os.Stdout, so that output has to be intercepted.
//
// This runs fn with os.Stdout redirected to a pipe and returns everything
// fn printed, split into lines. os.Stdout is always restored, on success
// or panic.
//
// The read end is drained on its own goroutine, concurrently with fn.
// That is required, not an optimisation: a pipe buffer is about 64 KiB, so
// a large "show running-config" would block fn forever waiting for a
// reader that had not started.
//
// Only a Pageable command is run through this. A handler that prompts
// partway through would have its prompt swallowed into the buffer instead
// of reaching the terminal, so such a command MUST NOT be marked Pageable.
func CaptureOutput(fn func()) ([]string, error) {
	real := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	os.Stdout = w
	defer func() { os.Stdout = real }()

	var buf bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		_, cerr := io.Copy(&buf, r)
		copyDone <- cerr
	}()

	fn()

	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := <-copyDone; err != nil {
		return nil, err
	}

	var lines []string
	scanner := bufio.NewScanner(&buf)
	// A single line can exceed bufio.Scanner's 64 KiB default, a large
	// "show running-config" for instance, so the buffer is grown well past
	// it rather than truncating or erroring on a long line.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, nil
}
