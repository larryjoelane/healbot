// Package fix opens a pull request for a human to review. It has no merge API.
package fix

import (
	"context"
	"fmt"
	"strings"

	"github.com/larryjoelane/healbot/internal/model"
)

// File is one path in the proposed change.
// Update is true when the path already exists and the provider should replace it.
type File struct {
	Path    string
	Content string
	Update  bool
}

// Draft is a branch and a description. Providers create it. They do not merge it.
type Draft struct {
	Title  string
	Body   string
	Branch string
	Files  []File
}

// Provider creates a pull request in a host. Implementations have no merge method.
type Provider interface {
	Name() string
	Create(ctx context.Context, draft Draft) (model.PullRequestRef, error)
}

// Propose builds the branch contents. The patch is a suggestion for review.
func Propose(inc model.Incident, tier2Outcome string) Draft {
	branch := "healbot/" + sanitize(inc.ID)
	title, files := filesFor(inc)
	body := renderBody(inc, tier2Outcome, title, "template")
	return Draft{Title: title, Body: body, Branch: branch, Files: files}
}

func filesFor(inc model.Incident) (string, []File) {
	frame := inc.TopFrame
	if frame == "" {
		frame = "the failing frame"
	}
	exc := inc.ExceptionType
	if exc == "" {
		exc = "the exception"
	}
	if strings.Contains(inc.ExceptionType, "NullReference") || strings.Contains(frame, "OrderService.Place") {
		title := "fix(orders): guard null customer in OrderService.Place"
		return title, []File{
			{
				Path: "src/Example.Orders/OrderService.cs",
				Content: `namespace Example.Orders;

public sealed class OrderService
{
    public Order Place(Cart cart)
    {
        ArgumentNullException.ThrowIfNull(cart);
        ArgumentNullException.ThrowIfNull(cart.Customer);
        return new Order(cart.Customer.Id, cart.Lines);
    }
}
`,
			},
			{
				Path: "tests/Example.Orders.Tests/OrderServiceTests.cs",
				Content: `using Xunit;

namespace Example.Orders.Tests;

public class OrderServiceTests
{
    [Fact]
    public void Place_WhenCustomerMissing_ThrowsArgumentNullException()
    {
        var svc = new OrderService();
        var cart = new Cart { Customer = null, Lines = new[] { "sku-1" } };
        var ex = Assert.Throws<ArgumentNullException>(() => svc.Place(cart));
        Assert.Equal("Customer", ex.ParamName);
    }
}
`,
			},
		}
	}
	if looksPython(inc) {
		title := "fix: guard the failing " + frame + " path"
		return title, []File{
			{Path: "healbot_proposal/fix.py", Content: "# Proposed guard for " + exc + " at " + frame + "\n"},
			{Path: "tests/test_healbot_regression.py", Content: "def test_" + sanitize(frame) + "_rejects_missing_input():\n    \"\"\"Regression for " + exc + ".\"\"\"\n    raise NotImplementedError('reviewer: assert the failure mode from incident " + inc.ID + "')\n"},
		}
	}
	title := "fix: address " + exc + " in " + frame
	return title, []File{
		{
			Path:    "healbot/proposals/" + sanitize(inc.ID) + ".md",
			Content: "# Proposed fix\n\nIncident `" + inc.ID + "` failed at `" + frame + "` with `" + exc + "`.\n\nReview the stack and add a guard at the first frame that dereferences the missing value.\n",
		},
		{
			Path:    "healbot/proposals/" + sanitize(inc.ID) + ".test.md",
			Content: "# Regression test plan\n\n- Reproduce `" + exc + "` at `" + frame + "`.\n- Assert the operation `" + inc.Operation + "` rejects the empty input instead of throwing.\n- Confirm the error rate returns below the alert threshold after the fix.\n",
		},
	}
}

func renderBody(inc model.Incident, outcome, title, mode string) string {
	var b strings.Builder
	b.WriteString("## Healbot diagnosis\n\n")
	b.WriteString("This pull request was opened for a human review. **Healbot does not merge pull requests.**\n\n")
	if mode == "" {
		mode = "template"
	}
	fmt.Fprintf(&b, "- Incident: `%s`\n- Resource: `%s`\n- Severity: `%s`\n- Tier 2 outcome: `%s`\n- Fix mode: `%s`\n\n", inc.ID, inc.ResourceID, inc.Severity, outcome, mode)
	if inc.Hypothesis != nil {
		fmt.Fprintf(&b, "### Hypothesis\n\n%s\n\nConfidence %.0f%% via %s. Category `%s`.\n\n", inc.Hypothesis.Summary, inc.Hypothesis.Confidence*100, inc.Hypothesis.Provider, inc.Hypothesis.Category)
		if len(inc.Hypothesis.Evidence) > 0 {
			b.WriteString("Evidence:\n\n")
			for _, e := range inc.Hypothesis.Evidence {
				b.WriteString("- ")
				b.WriteString(redact(e))
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	if inc.StackTrace != "" {
		b.WriteString("### Stack\n\n```\n")
		b.WriteString(redact(inc.StackTrace))
		b.WriteString("\n```\n\n")
	}
	b.WriteString("### Why this is not auto-applied\n\n")
	b.WriteString("The patch and the regression test are generated from the stack frame. They are a starting point for the service owner. CI on this repository should run the new test before anyone merges.\n\n")
	b.WriteString("Suggested title: ")
	b.WriteString(title)
	b.WriteString("\n")
	return b.String()
}

func looksPython(inc model.Incident) bool {
	s := inc.StackTrace + inc.TopFrame
	return strings.Contains(s, ".py") || strings.Contains(s, "Traceback")
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == '_' || r == '.' || r == '/' || r == ' ' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "incident"
	}
	if len(out) > 60 {
		return out[:60]
	}
	return out
}
