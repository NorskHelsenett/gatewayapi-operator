package annotations

import (
	"slices"
	"strings"
	"testing"
)

func TestParseEnvoyPodAnnotations(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    []string
		wantErr string
	}{
		{name: "empty disables the feature", value: "", want: nil},
		{name: "trims, skips empty and duplicates", value: " nhn.no/splunkIndex, ,nhn.no/splunkSourcetype,nhn.no/splunkIndex", want: []string{"nhn.no/splunkIndex", "nhn.no/splunkSourcetype"}},
		{name: "invalid key", value: "nhn.no/splunk index", wantErr: "invalid annotation key"},
		{name: "prefix is not supported", value: "nhn.no/", wantErr: "invalid annotation key"},
		{name: "IPAM key is reserved", value: "ipam.vitistack.io/zone", wantErr: "reserved for IPAM"},
		{name: "too many keys", value: "nhn.no/a,nhn.no/b,nhn.no/c,nhn.no/d,nhn.no/e,nhn.no/f,nhn.no/g,nhn.no/h,nhn.no/i", wantErr: "at most 8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnvoyPodAnnotations(tt.value)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
