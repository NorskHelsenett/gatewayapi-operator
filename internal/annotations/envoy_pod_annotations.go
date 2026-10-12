package annotations

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// maxEnvoyPodAnnotations stays below the ceiling of 12: Gateway API allows 16 infrastructure annotations and IPAM uses up to 4.
const maxEnvoyPodAnnotations = 8

const (
	// AnnotationManagedEnvoyPodAnnotations records on the Gateway which infrastructure annotation keys the operator wrote.
	AnnotationManagedEnvoyPodAnnotations = "gatewayapi-operator.vitistack.io/envoy-pod-annotations"

	// MaxAnnotationValueLength is the Gateway API limit for an infrastructure annotation value.
	MaxAnnotationValueLength = 4096

	// maxAnnotationKeyLength is the Gateway API limit for an infrastructure annotation key.
	maxAnnotationKeyLength = 253
)

// ParseEnvoyPodAnnotations parses the comma-separated list of HTTPRoute annotation keys that are copied to
// Gateway.spec.infrastructure.annotations, which Envoy Gateway puts on the shared Envoy pods.
func ParseEnvoyPodAnnotations(value string) ([]string, error) {
	var keys []string
	for _, key := range strings.Split(value, ",") {
		key = strings.TrimSpace(key)
		if key == "" || slices.Contains(keys, key) {
			continue
		}
		if err := validateEnvoyPodAnnotationKey(key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if len(keys) > maxEnvoyPodAnnotations {
		return nil, fmt.Errorf("%d annotation keys configured, at most %d are allowed", len(keys), maxEnvoyPodAnnotations)
	}
	return keys, nil
}

// ParseManagedEnvoyPodAnnotations reads the keys recorded on a Gateway, skipping entries that are not safe to remove.
func ParseManagedEnvoyPodAnnotations(value string) []string {
	var keys []string
	for _, key := range strings.Split(value, ",") {
		key = strings.TrimSpace(key)
		if key != "" && !slices.Contains(keys, key) && validateEnvoyPodAnnotationKey(key) == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

func validateEnvoyPodAnnotationKey(key string) error {
	if len(key) > maxAnnotationKeyLength {
		return fmt.Errorf("annotation key %q is longer than %d characters", key, maxAnnotationKeyLength)
	}
	if errs := validation.IsQualifiedName(key); len(errs) > 0 {
		return fmt.Errorf("invalid annotation key %q: %s", key, strings.Join(errs, "; "))
	}
	// The operator removes managed keys that no route sets, which would wipe the IPAM settings.
	if strings.HasPrefix(key, "ipam.vitistack.io/") {
		return fmt.Errorf("annotation key %q is reserved for IPAM", key)
	}
	return nil
}
