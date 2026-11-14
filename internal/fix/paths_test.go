package fix

import (
	"testing"

	"github.com/larryjoelane/healbot/internal/model"
)

func TestExtractPathsFromStacks(t *testing.T) {
	inc := model.Incident{
		TopFrame: "OrderService.Place",
		StackTrace: "" +
			"at Example.Orders.OrderService.Place(OrderService.cs:88)\n" +
			"File \"/app/src/orders/service.py\", line 12, in place\n" +
			"orders/service.go:40\n" +
			"at place (src/orders/service.js:12:5)\n",
	}
	frames := extractPaths(inc)
	joined := map[string]bool{}
	for _, cands := range frames {
		for _, c := range cands {
			joined[c] = true
		}
	}
	for _, want := range []string{"OrderService.cs", "src/orders/service.py", "service.py", "orders/service.go", "src/orders/service.js"} {
		if !joined[want] {
			t.Fatalf("missing %s in %#v", want, frames)
		}
	}
}

func TestGlobDenyMatchesCIAndLockfiles(t *testing.T) {
	cases := []string{
		".github/workflows/ci.yml",
		"deploy/terraform/main.tf",
		"go.sum",
		".env",
		"config/secrets/token.txt",
	}
	for _, p := range cases {
		if !matchAny(defaultDeny, p) {
			t.Fatalf("%s should be denied", p)
		}
	}
	if matchAny(defaultDeny, "src/orders/service.go") {
		t.Fatal("source file was denied")
	}
}

func TestCleanPathRejectsTraversal(t *testing.T) {
	for _, p := range []string{"../x", "/etc/passwd", "a/../../b", ".git/config", `c:\windows\system.ini`} {
		if _, err := cleanPath(p); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
}
