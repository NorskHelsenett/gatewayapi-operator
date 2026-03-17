package annotations

import (
	"encoding/json"
	"errors"
	"strings"
)

type OverrideInfrastrucutre map[string][]string

func (m *OverrideInfrastrucutre) ConvertToGatewayAnnotation() string {
	if m == nil || *m == nil || len(*m) == 0 {
		return ""
	}

	data, err := json.Marshal(*m)
	if err != nil {
		return ""
	}

	return string(data)
}

func (m *OverrideInfrastrucutre) ParseAnnotation(fqdn string, annotation string) error {
	type overrideInfrastructure struct {
		Infrastructure []string `json:"infrastructure"`
	}

	parsed := overrideInfrastructure{}
	if err := json.Unmarshal([]byte(annotation), &parsed); err != nil {
		return err
	}
	for _, v := range parsed.Infrastructure {
		if strings.ToLower(v) != "helsenett" && strings.ToLower(v) != "internett" {
			return errors.New("Value of override-infrastructure annotaion on HTTPRoute is not valid")
		}
	}

	if *m == nil {
		*m = make(OverrideInfrastrucutre)
	}

	(*m)[fqdn] = append((*m)[fqdn], parsed.Infrastructure...)
	return nil
}

func NewOverrideInfrastructure() OverrideInfrastrucutre {
	return make(OverrideInfrastrucutre, 0)
}
