// SPDX-License-Identifier: Apache-2.0

package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubjectCovers(t *testing.T) {
	for _, tc := range []struct {
		pattern, subject string
		want             bool
	}{
		{"cpms.>", "cpms.>", true},
		{">", "cpms.>", true},
		{"*.>", "cpms.>", true},
		{"cpms.>", "cpms.session.completed.org", true},
		{"cpms.*.*.*", "cpms.session.completed.org", true},
		{"cpms.*", "cpms.*", true},
		{"cpms.*", "cpms.>", false},
		{"cpms.*.>", "cpms.>", false},
		{"cpms.session.>", "cpms.>", false},
		{"cpms.>", "cpms", false},
		{"cpms", "cpms.>", false},
		{"other.>", "cpms.>", false},
		{"cpms.a", "cpms.*", false},
		{"cpms.a.b", "cpms.a", false},
	} {
		assert.Equal(t, tc.want, subjectCovers(tc.pattern, tc.subject), "%s covers %s", tc.pattern, tc.subject)
	}
}
