package annotations

import (
	"encoding/json"
)

type IgnoreDnsUpdates map[string]bool

func (m *IgnoreDnsUpdates) ConvertToGatewayAnnotation() string {
	if m == nil || *m == nil {
		return "{}"
	}

	data, err := json.Marshal(*m)
	if err != nil {
		return "{}"
	}

	return string(data)

}

func (m *IgnoreDnsUpdates) ParseAnnotation(fqdn string, annotation string) error {
	if annotation != "false" {
		if *m == nil {
			*m = make(IgnoreDnsUpdates)
		}
		(*m)[fqdn] = true
		return nil
	}
	return nil
}
