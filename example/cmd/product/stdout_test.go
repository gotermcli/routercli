// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package product

import (
	"testing"

	"github.com/gotermcli/routercli/internal/clitest"
)

// captureStdout - This function is a thin alias for clitest.CaptureStdout,
// kept so this package's tests read the same way they always have. The
// real implementation is shared with every other command package's tests;
// see internal/clitest for why it lives there rather than being copied
// into each package.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return clitest.CaptureStdout(t, fn)
}
