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
	// Check if someone misspelled "true" or if it is not set
	if annotation != "false" && annotation != "" {
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
