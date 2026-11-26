package domainname

import "testing"

func TestNormalize(t *testing.T) {
	for _, input := range []string{" пример.рф. ", "XN--E1AFMKFD.XN--P1AI"} {
		got, err := Normalize(input)
		if err != nil || got != "xn--e1afmkfd.xn--p1ai" {
			t.Errorf("Normalize(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "localhost", "127.0.0.1", "https://example.com", "*.example.com", "example.com:443", "a..com", "a.com..", "-a.com"} {
		if got, err := Normalize(input); err == nil {
			t.Errorf("Normalize(%q) = %q; want error", input, got)
		}
	}
}
