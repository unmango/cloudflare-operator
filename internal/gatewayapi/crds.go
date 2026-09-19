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

// Package gatewayapi locates the Gateway API CRDs that envtest, kind, and the
// conformance suite install.
package gatewayapi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Module is the Go module carrying both the Gateway API types and their CRDs.
const Module = "sigs.k8s.io/gateway-api"

// DirectoryEnvVar overrides the located directory, for callers outside a
// module cache.
const DirectoryEnvVar = "GATEWAY_API_CRDS"

// CRDDirectory returns the standard channel CRD directory.
//
// The CRDs ship inside the Go module, so the YAML installed into a cluster
// always matches the types the controllers compile against, and locating them
// needs no network.
func CRDDirectory() (string, error) {
	if dir, ok := os.LookupEnv(DirectoryEnvVar); ok {
		return dir, nil
	}

	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", Module).Output()
	if err != nil {
		return "", fmt.Errorf("locating %s: %w", Module, err)
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("locating %s: module has no directory", Module)
	}

	return filepath.Join(dir, "config", "crd", "standard"), nil
}
