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

func TestNormalizeProjectName(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"Estate 105, Noida", "Estate105"},
		{"Estate 360, Gurugram", "Estate 360"},
		{"Estate 361, Gurugram", "Estate 361 Residences"},
		{"The Terraces, Gurugram", "Estate 361 - The Terraces"},
		{"Max District", "Max District"},
		{"Max Square Two", "Max Square - Two"},
		{"The Bungalows", "The Bungalows"},
		{"222 Rajpur, Dehradun", "222 Rajpur"},
		{"Max One (Office)", "MAX One"},
		{"", ""},
		{"Some Unknown Project, City", "Some Unknown Project"},
	}
	for _, c := range cases {
		if got := normalizeProjectName(c.raw); got != c.want {
			t.Errorf("normalizeProjectName(%q) = %q, want %q", c.raw, got, c.want)
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
