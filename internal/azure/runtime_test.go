package azure

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/larryjoelane/healbot/internal/model"
)

type staticCred struct{}

func (staticCred) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "test-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestRestartAndDeploymentsUseARM(t *testing.T) {
	const id = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Web/sites/example-api"
	var paths []string
	opts := &arm.ClientOptions{}
	opts.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.Method+" "+req.URL.Path)
		if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing bearer token on %s", req.URL.Path)
		}
		switch {
		case strings.HasSuffix(req.URL.Path, "/restart"):
			return jsonResponse(req, 200, ""), nil
		case strings.HasSuffix(req.URL.Path, "/slotsswap"):
			return jsonResponse(req, 200, `{}`), nil
		case strings.HasSuffix(req.URL.Path, "/deployments"):
			return jsonResponse(req, 200, `{"value":[{"name":"v1.4.2","properties":{"active":true,"startTime":"2026-11-13T13:50:00Z"}}]}`), nil
		default:
			t.Errorf("unexpected %s %s", req.Method, req.URL.Path)
			return jsonResponse(req, 500, `{}`), nil
		}
	})
	rt, err := NewRuntime("sub", staticCred{}, "", opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := rt.Restart(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := rt.SwapSlot(ctx, id, "staging"); err != nil {
		t.Fatal(err)
	}
	deps, err := rt.ListChanges(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].Name != "v1.4.2" || !deps[0].Active {
		t.Fatalf("%+v", deps)
	}
	joined := strings.Join(paths, "\n")
	for _, want := range []string{"/restart", "/slotsswap", "/deployments"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
}

func TestQuerySignals(t *testing.T) {
	opts := &arm.ClientOptions{}
	opts.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/query") {
			t.Fatalf("path %s", req.URL.Path)
		}
		return jsonResponse(req, 200, `{
		  "tables": [{
		    "name": "PrimaryResult",
		    "columns": [
		      {"name": "timestamp", "type": "datetime"},
		      {"name": "problemId", "type": "string"},
		      {"name": "outerMessage", "type": "string"},
		      {"name": "ResourceId", "type": "string"}
		    ],
		    "rows": [[
		      "2026-11-13T14:02:01Z",
		      "System.NullReferenceException",
		      "Object reference not set",
		      "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Web/sites/example-api"
		    ]]
		  }]
		}`), nil
	})
	rt, err := NewRuntime("sub", staticCred{}, "workspace-1", opts)
	if err != nil {
		t.Fatal(err)
	}
	sigs, err := rt.QuerySignals(context.Background(), "exceptions | take 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sigs) != 1 || sigs[0].ExceptionType != "System.NullReferenceException" || sigs[0].ResourceName != "example-api" {
		t.Fatalf("%+v", sigs)
	}
	if sigs[0].ResourceType != model.TypeWebSite {
		t.Fatal(sigs[0].ResourceType)
	}
}

func TestWebHealthRequiresProbe(t *testing.T) {
	opts := &arm.ClientOptions{}
	opts.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("health must not call ARM for a web app, got %s", req.URL.Path)
		return nil, nil
	})
	rt, err := NewRuntime("sub", staticCred{}, "", opts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.Health(context.Background(), "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Web/sites/example-api")
	if err == nil || !strings.Contains(err.Error(), "health_url") {
		t.Fatal(err)
	}
}
