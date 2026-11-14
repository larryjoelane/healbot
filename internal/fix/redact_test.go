package fix

import (
	"strings"
	"testing"
)

func TestRedactTokensAndConnectionStrings(t *testing.T) {
	secret := strings.Repeat("A", 44)
	raw := strings.Join([]string{
		"Default" + "EndpointsProtocol=https;AccountName=ex;Account" + "Key=" + secret,
		"Authorization: Bearer " + strings.Repeat("b", 32),
		"ghp_" + strings.Repeat("c", 36),
		"password=" + strings.Repeat("d", 12),
	}, " ")
	out := redact(raw)
	for _, part := range []string{secret, strings.Repeat("b", 32), strings.Repeat("c", 36), strings.Repeat("d", 12)} {
		if strings.Contains(out, part) {
			t.Fatalf("secret leaked: %s", out)
		}
	}
	if strings.Count(out, "[REDACTED]") < 3 {
		t.Fatal(out)
	}
}
