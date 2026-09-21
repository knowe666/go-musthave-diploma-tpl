package api

import "testing"

func TestIsDigits(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "digits", value: "123456", want: true},
		{name: "empty", value: "", want: false},
		{name: "letters", value: "123a", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isDigits(test.value); got != test.want {
				t.Fatalf("isDigits(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestLuhnValid(t *testing.T) {
	if !luhnValid("79927398713") {
		t.Fatal("expected valid Luhn number")
	}
	if luhnValid("79927398714") {
		t.Fatal("expected invalid Luhn number")
	}
}

func TestNormalizeAccrualStatus(t *testing.T) {
	tests := map[string]string{
		"REGISTERED": "NEW",
		"PROCESSING": "PROCESSING",
		"INVALID":    "INVALID",
		"PROCESSED":  "PROCESSED",
		"UNKNOWN":    "NEW",
	}

	for input, want := range tests {
		if got := normalizeAccrualStatus(input); got != want {
			t.Errorf("normalizeAccrualStatus(%q) = %q, want %q", input, got, want)
		}
	}
}
