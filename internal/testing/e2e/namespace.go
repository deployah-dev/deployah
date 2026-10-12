// Copyright 2026 The Deployah Authors
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

package e2e

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// KindContext is the kube context of the suite's Kind cluster.
	KindContext = "kind-deployah"

	// RunAnnotation stores this execution's id on the namespace.
	RunAnnotation = "deployah.dev/e2e-run"

	// NamespaceReconcileAttempts is how many Gets follow an ambiguous create.
	NamespaceReconcileAttempts = 4

	// NamespaceReconcileInterval is the pause between those Gets.
	NamespaceReconcileInterval = 500 * time.Millisecond
)

// ErrNotFound means a Get found no object.
var ErrNotFound = errors.New("not found")

// ErrNotOwner means the live object is not this execution's claim.
var ErrNotOwner = errors.New("namespace is not owned by this run")

// ErrOwnershipUncertain means ownership was not proved, so nothing is deleted.
var ErrOwnershipUncertain = errors.New("namespace ownership uncertain")

// ErrNamespaceGone means a previously claimed namespace is already absent.
var ErrNamespaceGone = errors.New("namespace already deleted")

// CreateClass is the caller's classification of a namespace Create error.
type CreateClass int

const (
	// CreateSucceeded means Create returned nil.
	CreateSucceeded CreateClass = iota
	// CreateAlreadyExists means the API reported the name is taken.
	CreateAlreadyExists
	// CreateAmbiguous means the call failed and the object might still exist.
	CreateAmbiguous
	// CreateRejected means the API refused the object and did not store it.
	CreateRejected
)

// NamespaceView is the ownership data from one Get or Create response.
type NamespaceView struct {
	Name string
	UID  string
	Run  string
}

// Claim is one execution's proof that it created a namespace.
type Claim struct {
	Name  string
	RunID string
	UID   string
}

// CreateDecision is the result of comparing a Create outcome with one Get.
type CreateDecision struct {
	Owned bool
	Claim Claim
	Retry bool
	Err   error
}

// ResolveNamespaceCreate decides whether this execution owns the namespace.
func ResolveNamespaceCreate(runID, name string, class CreateClass, got *NamespaceView, getErr error) CreateDecision {
	// Retry means Get again. This function never deletes.
	switch class {
	case CreateRejected:
		return CreateDecision{Err: fmt.Errorf("namespace %s: create rejected", name)}
	case CreateSucceeded, CreateAlreadyExists, CreateAmbiguous:
		if claim, ok := ownedView(runID, name, got); ok && getErr == nil {
			return CreateDecision{Owned: true, Claim: claim}
		}
		if class != CreateSucceeded && getErr == nil && got != nil && !sameRun(runID, name, got) {
			return CreateDecision{Err: fmt.Errorf("namespace %s: %w", name, ErrNotOwner)}
		}
		return CreateDecision{Retry: true}
	default:
		return CreateDecision{Err: fmt.Errorf("namespace %s: unknown create result", name)}
	}
}

// FinishUncertain reports that reconciliation ended without proof of ownership.
func FinishUncertain(name string, createErr, getErr error) error {
	return fmt.Errorf("namespace %s: ownership uncertain, not deleting: %w",
		name, errors.Join(ErrOwnershipUncertain, createErr, getErr))
}

// ConfirmNamespaceOwner checks that got is still the claimed namespace.
func ConfirmNamespaceOwner(claim Claim, got *NamespaceView, getErr error) error {
	if claim.Name == "" || claim.RunID == "" || claim.UID == "" {
		return fmt.Errorf("namespace %s: %w", claim.Name, ErrOwnershipUncertain)
	}
	if errors.Is(getErr, ErrNotFound) {
		return ErrNamespaceGone
	}
	if getErr != nil {
		return fmt.Errorf("namespace %s: %w: %w", claim.Name, ErrOwnershipUncertain, getErr)
	}
	if got == nil || got.Name != claim.Name || got.Run != claim.RunID || got.UID != claim.UID {
		return fmt.Errorf("namespace %s: %w", claim.Name, ErrNotOwner)
	}
	return nil
}

// UIDPrecondition returns the UID a delete must send.
// A replaced object with a different UID is left alone.
func UIDPrecondition(uid string) (metav1.Preconditions, error) {
	if uid == "" {
		return metav1.Preconditions{}, errors.New("uid precondition requires a recorded uid")
	}
	parsed := types.UID(uid)
	return metav1.Preconditions{UID: &parsed}, nil
}

func ownedView(runID, name string, got *NamespaceView) (Claim, bool) {
	if !sameRun(runID, name, got) || got.UID == "" {
		return Claim{}, false
	}
	return Claim{Name: name, RunID: runID, UID: got.UID}, true
}

func sameRun(runID, name string, got *NamespaceView) bool {
	return got != nil && got.Name == name && got.Run == runID && runID != ""
}
