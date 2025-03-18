package connector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseApplicationsEntitlementIdUnmarshalString(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected *enterpriseApplicationsEntitlementId
	}{
		{
			name:  "assigment",
			input: "enterprise_application:890c333e-88e1-44f4-ac21-50785d3e89d1:assignment:00000000-0000-0000-0000-000000000000",
			expected: &enterpriseApplicationsEntitlementId{
				AppRoleId: "00000000-0000-0000-0000-000000000000",
				Type:      "assignment",
			},
		},
		{
			name:  "owner",
			input: "enterprise_application:cabcf2d8-d4b5-47cb-b590-dd87648d6604:owners",
			expected: &enterpriseApplicationsEntitlementId{
				AppRoleId: "",
				Type:      "owners",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var e enterpriseApplicationsEntitlementId
			err := e.UnmarshalString(test.input)
			require.NoError(t, err)

			require.Equal(t, test.expected, &e)
		})
	}
}
