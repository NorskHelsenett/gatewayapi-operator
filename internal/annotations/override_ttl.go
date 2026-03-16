package annotations

import (
	"encoding/json"
	"strconv"
)

type OverrideTtl map[string]int64

func (m *OverrideTtl) ConvertToGatewayAnnotation() string {
	if m == nil || *m == nil {
		return "{}"
	}

	data, err := json.Marshal(*m)
	if err != nil {
		return "{}"
	}

	return string(data)
}

func (m *OverrideTtl) ParseAnnotation(fqdn string, annotation string) error {

	// Parse the "dns.nhn.no/override-ttl: '<int>'"" if present on the HTTProute
	if annotation != "" {
		parsed, err := strconv.ParseInt(annotation, 10, 64)
		if err != nil {
			return err
		}
		if *m == nil {
			*m = make(OverrideTtl)
		}
		(*m)[fqdn] = parsed
		return nil
	}
	return nil
}
