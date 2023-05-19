/*
Copyright 2021 VMware, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"fmt"
	"strings"

	k8sfield "k8s.io/apimachinery/pkg/util/validation/field"
)

func ErrMissingFieldWithDetail(field string, detail string) FieldErrors {
	return FieldErrors{
		k8sfield.Required(k8sfield.NewPath(field), detail),
	}
}

func ErrMultipleSources(names ...string) FieldErrors {
	return FieldErrors{
		k8sfield.Required(k8sfield.NewPath(fmt.Sprintf("[%s]", strings.Join(names, ", "))), "expected exactly one, got multiple"),
	}
}
