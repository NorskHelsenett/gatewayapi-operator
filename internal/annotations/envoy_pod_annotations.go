package annotations

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// maxEnvoyPodAnnotations stays below the ceiling of 12: Gateway API allows 16 infrastructure annotations and IPAM uses up to 4.
const maxEnvoyPodAnnotations = 8

// ParseEnvoyPodAnnotations parses the comma-separated list of HTTPRoute annotation keys that are copied to
// Gateway.spec.infrastructure.annotations, which Envoy Gateway puts on the shared Envoy pods.
func ParseEnvoyPodAnnotations(value string) ([]string, error) {
	var keys []string
	for _, key := range strings.Split(value, ",") {
		key = strings.TrimSpace(key)
		if key == "" || slices.Contains(keys, key) {
			continue
		}
		if errs := validation.IsQualifiedName(key); len(errs) > 0 {
			return nil, fmt.Errorf("invalid annotation key %q: %s", key, strings.Join(errs, "; "))
		}
		// The operator removes configured keys that no route sets, which would wipe the IPAM settings.
		if strings.HasPrefix(key, "ipam.vitistack.io/") {
			return nil, fmt.Errorf("annotation key %q is reserved for IPAM", key)
		}
		keys = append(keys, key)
	}
	if len(keys) > maxEnvoyPodAnnotations {
		return nil, fmt.Errorf("%d annotation keys configured, at most %d are allowed", len(keys), maxEnvoyPodAnnotations)
	}
	return keys, nil
}
