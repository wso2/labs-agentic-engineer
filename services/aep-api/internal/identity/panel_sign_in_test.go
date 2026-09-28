// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package identity

import (
	"context"
	"errors"
	"testing"
)

type fakeCoords struct{ c SignInCoords }

func (f fakeCoords) SignInCoordinates(context.Context, string, string) (SignInCoords, error) {
	return f.c, nil
}

// The view names the client a person signs in AS and the issuer to sign in AT:
// the two public values a client outside the project (the test app) cannot
// learn any other way. Both come off the binding, so the read does not need
// the directory — which is exactly when the console most needs to keep
// offering a way in.
func TestPanelView_CarriesSignInCoordinatesOffTheBindingEvenWithoutTheDirectory(t *testing.T) {
	p := NewPanelService(&fakeTargets{err: errors.New("no admin credential")}, newFakeStore())
	p.SetSignInCoordinates(fakeCoords{SignInCoords{ClientID: "project-client", Resource: "https://aep.wso2.com/orgs/acme/projects/workouts", Issuer: "http://project-idp"}})

	view, err := p.View(context.Background(), "acme", "workouts")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if view.DirectoryAvailable {
		t.Fatal("DirectoryAvailable = true; the fixture's directory is down")
	}
	if view.SignIn == nil || view.SignIn.ClientID != "project-client" || view.SignIn.Issuer != "http://project-idp" {
		t.Fatalf("SignIn = %+v; want issuer http://project-idp and client project-client", view.SignIn)
	}
}

// A binding that predates the issuer output still answers, from the directory.
func TestPanelView_FallsBackToTheDirectoryIssuer(t *testing.T) {
	p := NewPanelService(newFakeTargets(newFakeDirectory()), newFakeStore())
	p.SetSignInCoordinates(fakeCoords{SignInCoords{ClientID: "project-client"}})

	view, err := p.View(context.Background(), "acme", "workouts")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if view.SignIn == nil || view.SignIn.Issuer != testIssuer {
		t.Fatalf("SignIn = %+v; want issuer %q", view.SignIn, testIssuer)
	}
}

// No sign-in resource means no block — never an empty client id a reader would
// build a broken authorize URL from.
func TestPanelView_OmitsSignInWithoutAClient(t *testing.T) {
	p := NewPanelService(newFakeTargets(newFakeDirectory()), newFakeStore())
	p.SetSignInCoordinates(fakeCoords{})

	view, err := p.View(context.Background(), "acme", "workouts")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if view.SignIn != nil {
		t.Fatalf("SignIn = %+v; want nil", view.SignIn)
	}
}
