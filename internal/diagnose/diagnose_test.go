package diagnose

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
)

func TestHeuristicNullRefWithDeploy(t *testing.T) {
	inc := model.Incident{
		ResourceType:  model.TypeWebSite,
		ExceptionType: "System.NullReferenceException",
		TopFrame:      "OrderService.Place",
		Message:       "Object reference not set to an instance of an object.",
		ErrorRate:     0.184,
	}
	bundle := model.ContextBundle{RecentDeploy: true, Deployments: []model.Deployment{{
		Name: "v1.4.2", Active: true, At: time.Now().Add(-12 * time.Minute),
	}}}
	h, err := Heuristic{}.Diagnose(context.Background(), inc, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !h.LikelyCode || h.Category != model.CategoryCode || h.SuggestedAction != model.ActionSlotSwap {
		t.Fatalf("%+v", h)
	}
	if h.Confidence < 0.8 {
		t.Fatal(h.Confidence)
	}
}

func TestHeuristicCapacity(t *testing.T) {
	h, err := Heuristic{}.Diagnose(context.Background(), model.Incident{
		Message: "CPU saturation on the plan", ResourceType: model.TypeWebSite,
	}, model.ContextBundle{})
	if err != nil {
		t.Fatal(err)
	}
	if h.LikelyCode || h.SuggestedAction != model.ActionScaleOut || h.Category != model.CategoryCapacity {
		t.Fatalf("%+v", h)
	}
}

func TestLLMOverridesHeuristic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "k" {
			t.Fatal("missing key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"null customer after swap\",\"confidence\":0.91,\"category\":\"code\",\"likely_code\":true,\"suggested_action\":\"slot_swap\",\"evidence\":[\"model\"]}"}}]}`))
	}))
	defer srv.Close()
	svc := Service{LLM: &AzureOpenAI{Endpoint: srv.URL, Deployment: "gpt", Key: "k", HTTP: srv.Client()}}
	h, err := svc.Diagnose(context.Background(), model.Incident{
		ExceptionType: "System.NullReferenceException", Message: "Object reference not set", ResourceType: model.TypeWebSite,
	}, model.ContextBundle{RecentDeploy: true})
	if err != nil {
		t.Fatal(err)
	}
	if h.Provider != "azure_openai" || !strings.Contains(h.Summary, "null customer") {
		t.Fatalf("%+v", h)
	}
	if h.SuggestedAction != model.ActionSlotSwap {
		t.Fatal(h.SuggestedAction)
	}
}

func TestLLMFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", 500)
	}))
	defer srv.Close()
	svc := Service{LLM: &AzureOpenAI{Endpoint: srv.URL, Deployment: "gpt", Key: "k", HTTP: srv.Client()}}
	h, err := svc.Diagnose(context.Background(), model.Incident{Message: "CPU saturation"}, model.ContextBundle{})
	if err != nil {
		t.Fatal(err)
	}
	if h.Provider != "heuristic" || h.Category != model.CategoryCapacity {
		t.Fatalf("%+v", h)
	}
}
