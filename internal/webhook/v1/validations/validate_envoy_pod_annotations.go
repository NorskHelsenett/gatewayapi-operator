package validations

import (
	"cmp"
	"fmt"
	"slices"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ValidateEnvoyPodAnnotations rejects a value that differs from the one used by the oldest other HTTPRoute on the same Gateway.
func ValidateEnvoyPodAnnotations(keys []string, httproute *gatewayv1.HTTPRoute, otherRoutesOnGateway []gatewayv1.HTTPRoute) error {
	sorted := slices.SortedStableFunc(slices.Values(otherRoutesOnGateway), func(a, b gatewayv1.HTTPRoute) int {
		return cmp.Or(
			a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})

	for _, key := range keys {
		value, ok := httproute.Annotations[key]
		if !ok {
			continue
		}
		for _, other := range sorted {
			existing, found := other.Annotations[key]
			if !found {
				continue
			}
			if existing != value {
				return fmt.Errorf("HTTPRoute annotation %s=%q conflicts with %q on HTTPRoute %s/%s, which uses the same Gateway",
					key, value, existing, other.Namespace, other.Name)
			}
			break
		}
	}
	return nil
}
