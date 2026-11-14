package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/larryjoelane/healbot/internal/model"
)

// Service prefers an LLM and falls back to the heuristic when the model is
// unavailable or returns something that is not the expected JSON object.
type Service struct {
	Heuristic Heuristic
	LLM       *AzureOpenAI
}

// Diagnose implements Diagnoser.
func (s Service) Diagnose(ctx context.Context, inc model.Incident, bundle model.ContextBundle) (model.Hypothesis, error) {
	base, err := s.Heuristic.Diagnose(ctx, inc, bundle)
	if err != nil || s.LLM == nil {
		return base, err
	}
	raw, err := s.LLM.Complete(ctx, systemPrompt, userPrompt(inc, bundle, base))
	if err != nil {
		base.Evidence = append(base.Evidence, "llm unavailable: "+err.Error())
		return base, nil
	}
	parsed, err := parseHypothesis(raw)
	if err != nil {
		base.Evidence = append(base.Evidence, "llm response discarded: "+err.Error())
		return base, nil
	}
	parsed.Evidence = append(parsed.Evidence, base.Evidence...)
	if parsed.Provider == "" {
		parsed.Provider = "azure_openai"
	}
	return parsed, nil
}

const systemPrompt = `You are Healbot, an incident diagnoser for Azure applications.
Respond with a single JSON object and no markdown:
{"summary":"","confidence":0.0,"category":"code|deployment|capacity|dependency|config|unknown","likely_code":false,"suggested_action":"restart|scale_out|slot_swap|restart_revision|rollback_revision|restart_consumer|","evidence":[]}
suggested_action must be one of those reversible runbooks or an empty string.
Do not invent shell commands. Do not suggest deleting resources.`

func userPrompt(inc model.Incident, bundle model.ContextBundle, base model.Hypothesis) string {
	var b strings.Builder
	fmt.Fprintf(&b, "resource: %s\nseverity: %s\nexception: %s\nframe: %s\noperation: %s\nmessage: %s\nstack: %s\nerror_rate: %v\nrecent_deploy: %v\nheuristic: %s\n",
		inc.ResourceID, inc.Severity, inc.ExceptionType, inc.TopFrame, inc.Operation, inc.Message, trim(inc.StackTrace, 1500), inc.ErrorRate, bundle.RecentDeploy, base.Summary)
	for _, d := range bundle.Deployments {
		fmt.Fprintf(&b, "deployment %s slot=%s revision=%s active=%v at=%s\n", d.Name, d.Slot, d.Revision, d.Active, d.At.Format(time.RFC3339))
	}
	return b.String()
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func parseHypothesis(raw string) (model.Hypothesis, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	var body struct {
		Summary         string   `json:"summary"`
		Confidence      float64  `json:"confidence"`
		Category        string   `json:"category"`
		LikelyCode      bool     `json:"likely_code"`
		SuggestedAction string   `json:"suggested_action"`
		Evidence        []string `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &body); err != nil {
		return model.Hypothesis{}, err
	}
	if strings.TrimSpace(body.Summary) == "" {
		return model.Hypothesis{}, fmt.Errorf("empty summary")
	}
	cat := model.Category(body.Category)
	switch cat {
	case model.CategoryCode, model.CategoryDeployment, model.CategoryCapacity, model.CategoryDependency, model.CategoryConfig, model.CategoryUnknown:
	default:
		return model.Hypothesis{}, fmt.Errorf("unknown category %q", body.Category)
	}
	action := model.ActionKind(body.SuggestedAction)
	if action != "" && !action.Valid() {
		return model.Hypothesis{}, fmt.Errorf("unknown action %q", body.SuggestedAction)
	}
	if body.Confidence < 0 {
		body.Confidence = 0
	}
	if body.Confidence > 1 {
		body.Confidence = 1
	}
	return model.Hypothesis{
		Summary:         body.Summary,
		Confidence:      body.Confidence,
		Category:        cat,
		LikelyCode:      body.LikelyCode,
		SuggestedAction: action,
		Evidence:        body.Evidence,
		Provider:        "azure_openai",
	}, nil
}

// AzureOpenAI calls an Azure OpenAI deployment. Prefer a TokenCredential from
// DefaultAzureCredential. An API key is accepted only when explicitly configured.
type AzureOpenAI struct {
	Endpoint   string
	Deployment string
	APIVersion string
	Key        string
	Cred       azcore.TokenCredential
	HTTP       *http.Client
}

// Complete returns the assistant text.
// Diagnosis and tier-3 model_patch share this method. The provider is the
// Azure OpenAI deployment configured on the process; other Completer
// implementations can be wired in without changing the HTTP client.
func (a *AzureOpenAI) Complete(ctx context.Context, system, user string) (string, error) {
	if a.Endpoint == "" || a.Deployment == "" {
		return "", fmt.Errorf("azure openai endpoint and deployment are required")
	}
	version := a.APIVersion
	if version == "" {
		version = "2024-10-21"
	}
	url := strings.TrimRight(a.Endpoint, "/") + "/openai/deployments/" + a.Deployment + "/chat/completions?api-version=" + version
	payload := map[string]any{
		"temperature": 0.1,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"response_format": map[string]string{"type": "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.Key != "" {
		req.Header.Set("api-key", a.Key)
	} else if a.Cred != nil {
		tok, err := a.Cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{"https://cognitiveservices.azure.com/.default"}})
		if err != nil {
			return "", fmt.Errorf("azure openai token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok.Token)
	} else {
		return "", fmt.Errorf("azure openai has no credential")
	}
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("azure openai status %d", res.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("azure openai returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}
