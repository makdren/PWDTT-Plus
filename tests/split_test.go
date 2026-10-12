package backend_test

import (
	"testing"

	"pwdtt/backend"
)

func TestNormalizeExclusion(t *testing.T) {
	cases := map[string][2]string{
		"https://www.Example.com/path?q=1": {"www.example.com", "domain"},
		"*.youtube.com":                    {"youtube.com", "domain"},
		"site.ru/page":                     {"site.ru", "domain"},
		"site.ru:443":                      {"site.ru", "domain"},
		"1.2.3.4":                          {"1.2.3.4", "ip"},
		"10.1.2.3/8":                       {"10.0.0.0/8", "cidr"},
		"::1":                              {"", ""},
		"not a site":                       {"", ""},
		"# comment":                        {"", ""},
		"localhost":                        {"", ""},
	}
	for in, want := range cases {
		got := backend.NormalizeExclusion(in)
		if got.Value != want[0] || got.Kind != want[1] {
			t.Errorf("%q: got %+v want %v", in, got, want)
		}
	}
}

func TestNormalizeExclusionsDedup(t *testing.T) {
	got := backend.NormalizeExclusions([]string{"a.ru", "https://a.ru/", "bad", "1.1.1.1"})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}
