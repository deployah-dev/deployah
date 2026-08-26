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

//go:build e2e

package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestIsRetryableAPIError(t *testing.T) {
	t.Parallel()

	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "web")
	conflict := apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "web", errors.New("conflict"))
	noMatch := &meta.NoKindMatchError{GroupKind: schema.GroupKind{Kind: "Widget"}}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "not found", err: notFound, want: true},
		{name: "wrapped not found", err: fmt.Errorf("get: %w", notFound), want: true},
		{name: "conflict", err: conflict, want: true},
		{name: "server timeout", err: apierrors.NewServerTimeout(schema.GroupResource{Resource: "pods"}, "get", 1), want: true},
		{name: "too many requests", err: apierrors.NewTooManyRequests("slow down", 1), want: true},
		{name: "service unavailable", err: apierrors.NewServiceUnavailable("down"), want: true},
		{name: "timeout status", err: apierrors.NewTimeoutError("timed out", 1), want: true},
		{name: "internal error", err: apierrors.NewInternalError(errors.New("boom")), want: true},
		{name: "connection refused", err: fmt.Errorf("dial: %w", syscall.ECONNREFUSED), want: true},
		{name: "connection reset", err: fmt.Errorf("read: %w", syscall.ECONNRESET), want: true},
		{name: "probable eof", err: io.EOF, want: true},
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "web", errors.New("no")), want: false},
		{name: "bad request", err: apierrors.NewBadRequest("nope"), want: false},
		{name: "no kind match", err: noMatch, want: false},
		{name: "wrapped rest mapping", err: fmt.Errorf("RESTMapping apps/v1, Kind=Widget: %w", noMatch), want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isRetryableAPIError(tt.err))
		})
	}
}

func TestFixtureNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "basic-web-service", want: "e2e-basic-web-service"},
		{name: "uppercase", in: "FooBar", want: "e2e-foobar"},
		{name: "underscores", in: "foo_bar", want: "e2e-foo-bar"},
		{name: "spaces become hyphens", in: "foo bar", want: "e2e-foo-bar"},
		{name: "truncates at 63", in: strings.Repeat("a", 80), want: "e2e-" + strings.Repeat("a", 59)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fixtureNamespace(tt.in)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, len(got), maxDNSLabel)
		})
	}
}
