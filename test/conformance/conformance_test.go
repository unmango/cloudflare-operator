//go:build conformance

/*
Copyright 2025.

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

// Package conformance runs the Gateway API conformance suite against the
// operator deployed in a kind cluster. `make test-conformance` sets the cluster
// up and passes the options file, hack/conformance/options.yaml, which names
// the GatewayClass and the features the operator claims.
package conformance

import (
	"testing"

	"sigs.k8s.io/gateway-api/conformance"
)

func TestConformance(t *testing.T) {
	conformance.RunConformanceWithOptions(t, conformance.DefaultOptions(t))
}
