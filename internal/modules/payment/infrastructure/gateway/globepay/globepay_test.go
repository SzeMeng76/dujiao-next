package globepay

import "testing"

func TestParseAmountToCents(t *testing.T) {
	tests := []struct {
		name    string
		amount  string
		want    int
		wantErr bool
	}{
		{name: "nine forty five", amount: "9.45", want: 945},
		{name: "nine forty four", amount: "9.44", want: 944},
		{name: "one cent", amount: "0.01", want: 1},
		{name: "whole amount", amount: "10.00", want: 1000},
		{name: "half cent rounds up", amount: "9.455", want: 946},
		{name: "negative", amount: "-1.00", wantErr: true},
		{name: "invalid", amount: "not-an-amount", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmountToCents(tt.amount)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseAmountToCents(%q) error = nil, want error", tt.amount)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAmountToCents(%q) error = %v", tt.amount, err)
			}
			if got != tt.want {
				t.Fatalf("parseAmountToCents(%q) = %d, want %d", tt.amount, got, tt.want)
			}
		})
	}
}
