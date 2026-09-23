// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package nabatctx provides Nabat application contexts and captured IO for tests.
package nabatctx

import (
	"bytes"
	"testing"

	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
)

// Harness contains a test application context and its captured IO.
type Harness struct {
	Context *nabat.Context
	App     *nabat.App
	Stdin   *bytes.Buffer
	Stdout  *bytes.Buffer
	Stderr  *bytes.Buffer
}

// New creates a non-interactive test application named appName.
func New(tb testing.TB, appName string) *Harness {
	tb.Helper()

	appIO, in, out, errOut := nabattest.NewIO()
	app := nabat.MustNew(appName, nabat.WithIO(appIO))

	return &Harness{
		Context: nabattest.Context(tb, app),
		App:     app,
		Stdin:   in,
		Stdout:  out,
		Stderr:  errOut,
	}
}
