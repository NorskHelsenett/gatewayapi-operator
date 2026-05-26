package annotations

import (
	"strings"
)

type IgnoreDnsUpdates []string

func (m *IgnoreDnsUpdates) ConvertToGatewayAnnotation() string {
	if m == nil || *m == nil {
		return ""
	}

	return strings.Join(*m, ",")

}

func (m *IgnoreDnsUpdates) ParseAnnotation(fqdn string, annotation string) error {
	// Normalize annotation value and only accept "all" or "true".
	normalizedValue := strings.ToLower(strings.TrimSpace(annotation))
	// The dns.nhn.no/ignore annotations value was originally just "all" but due to a miscommunication in docs on docs.nhn.no we also have to support "true".
	// This is implemented the same way in gatewayapi-operator and HTTPRoutes. Both "all" and "true" are supported.
	if normalizedValue == "all" || normalizedValue == "true" {
		if *m == nil {
			*m = make(IgnoreDnsUpdates, 0, 1)
		}
		*m = append(*m, fqdn)
		return nil
	}
	return nil
}

func NewIgnoreDnsUpdates() IgnoreDnsUpdates {
	return make(IgnoreDnsUpdates, 0)
}
