package common

import "testing"

func TestIsDomainListed(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		list   []string
		want   bool
	}{
		{
			name:   "exact match",
			domain: "edu.cn",
			list:   []string{"edu.cn"},
			want:   true,
		},
		{
			name:   "exact match does not include subdomain",
			domain: "school.edu.cn",
			list:   []string{"edu.cn"},
			want:   false,
		},
		{
			name:   "wildcard matches root domain",
			domain: "edu.cn",
			list:   []string{"*.edu.cn"},
			want:   true,
		},
		{
			name:   "wildcard matches direct subdomain",
			domain: "school.edu.cn",
			list:   []string{"*.edu.cn"},
			want:   true,
		},
		{
			name:   "wildcard matches nested subdomain",
			domain: "mail.school.edu.cn",
			list:   []string{"*.edu.cn"},
			want:   true,
		},
		{
			name:   "wildcard ignores case and whitespace",
			domain: " School.EDU.CN ",
			list:   []string{" *.EDU.CN "},
			want:   true,
		},
		{
			name:   "wildcard does not match similar suffix",
			domain: "notedu.cn",
			list:   []string{"*.edu.cn"},
			want:   false,
		},
		{
			name:   "wildcard does not match dashed similar suffix",
			domain: "evil-edu.cn",
			list:   []string{"*.edu.cn"},
			want:   false,
		},
		{
			name:   "empty domain does not match",
			domain: " ",
			list:   []string{"*.edu.cn"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDomainListed(tt.domain, tt.list); got != tt.want {
				t.Fatalf("IsDomainListed(%q, %#v) = %v, want %v", tt.domain, tt.list, got, tt.want)
			}
		})
	}
}
