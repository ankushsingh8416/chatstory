package handlers

import "testing"

func TestIndianMobileRe(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"my number is 9876543210", "9876543210"},
		{"+91 9876543210 call me", "9876543210"},
		{"919876543210", "9876543210"},
		{"budget 10 Cr. - 15 Cr.", ""},
		{"Estate 361, Gurugram", ""},
	}
	for _, c := range cases {
		m := indianMobileRe.FindStringSubmatch(c.text)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != c.want {
			t.Errorf("indianMobileRe(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestLeadNameRe(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"my name is Rahul, 9876543210", "Rahul"},
		{"I'm Rahul Sharma", "Rahul Sharma"},
		{"9876543210", ""},
	}
	for _, c := range cases {
		got := ""
		if m := leadNameRe.FindStringSubmatch(c.text); m != nil {
			got = m[1]
		}
		if got != c.want {
			t.Errorf("leadNameRe(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}
