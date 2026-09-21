package storage

import (
	"database/sql"
	"testing"
)

func TestParseNullableFloat(t *testing.T) {
	tests := []struct {
		name  string
		value sql.NullString
		want  float64
		valid bool
	}{
		{name: "value", value: sql.NullString{String: "12.50", Valid: true}, want: 12.5, valid: true},
		{name: "null", value: sql.NullString{}, want: 0, valid: true},
		{name: "invalid", value: sql.NullString{String: "not-a-number", Valid: true}, valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseNullableFloat(test.value)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("parseNullableFloat(%v) = %v, %v; want %v, nil", test.value, got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatal("expected parsing error")
			}
		})
	}
}
