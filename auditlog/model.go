// Copyright 2026 Bret Jordan, All rights reserved.
//
// Use of this source code is governed by an Apache 2.0 license
// that can be found in the LICENSE file in the root of the source tree.

package auditlog

import (
	"os"
	"sync"

	"github.com/gologme/log"
)

// ----------------------------------------------------------------------
// Define Object Model
// ----------------------------------------------------------------------

// AuditLog - This type implements the audit trail. The file handle is
// opened lazily, so the constructor never touches the filesystem when
// auditing is disabled at startup.
type AuditLog struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	enabled bool
	logger  *log.Logger
}

// ----------------------------------------------------------------------
// Initialization Functions
// ----------------------------------------------------------------------

// New - This creates an AuditLog at path. The file is not opened until the
// first Enable call, so a deployment with auditing off never touches the
// filesystem for it.
//
// logger reports write failures that happen later, after the file is open.
// A nil logger is replaced with one writing to stderr, so omitting it
// cannot crash the constructor.
func New(path string, logger *log.Logger) *AuditLog {
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return &AuditLog{path: path, logger: logger}
}
