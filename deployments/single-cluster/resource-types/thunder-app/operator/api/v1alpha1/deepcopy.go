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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// Hand-written instead of controller-gen `object` output: every field on
// ThunderApplicationSpec/Status is a scalar (string/bool/int64), so their
// DeepCopyInto is a plain struct copy and generating a dedicated tool for it
// isn't worth the extra scaffolding. If any field gains a slice, map, or
// pointer, update these methods to deep-copy it explicitly (or reintroduce
// controller-gen object generation). The deepcopy round-trip test in
// thunderapplication_types_test.go guards field-copy completeness.

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ThunderApplication) DeepCopyInto(out *ThunderApplication) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy copies the receiver, creating a new ThunderApplication.
func (in *ThunderApplication) DeepCopy() *ThunderApplication {
	if in == nil {
		return nil
	}
	out := new(ThunderApplication)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *ThunderApplication) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ThunderApplicationList) DeepCopyInto(out *ThunderApplicationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ThunderApplication, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy copies the receiver, creating a new ThunderApplicationList.
func (in *ThunderApplicationList) DeepCopy() *ThunderApplicationList {
	if in == nil {
		return nil
	}
	out := new(ThunderApplicationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *ThunderApplicationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
// All fields are scalars, so a struct copy is a full deep copy.
func (in *ThunderApplicationSpec) DeepCopyInto(out *ThunderApplicationSpec) {
	*out = *in
}

// DeepCopy copies the receiver, creating a new ThunderApplicationSpec.
func (in *ThunderApplicationSpec) DeepCopy() *ThunderApplicationSpec {
	if in == nil {
		return nil
	}
	out := new(ThunderApplicationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
// All fields are scalars, so a struct copy is a full deep copy.
func (in *ThunderApplicationStatus) DeepCopyInto(out *ThunderApplicationStatus) {
	*out = *in
}

// DeepCopy copies the receiver, creating a new ThunderApplicationStatus.
func (in *ThunderApplicationStatus) DeepCopy() *ThunderApplicationStatus {
	if in == nil {
		return nil
	}
	out := new(ThunderApplicationStatus)
	in.DeepCopyInto(out)
	return out
}
