package ingest

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
)

// ParseMonitorAlert reads an Azure Monitor common alert schema payload and
// returns one signal per target resource.
func ParseMonitorAlert(r io.Reader, received time.Time) ([]model.Signal, error) {
	var env alertEnvelope
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("monitor alert: %w", err)
	}
	if env.SchemaID != "" && !strings.Contains(strings.ToLower(env.SchemaID), "azuremonitor") {
		return nil, fmt.Errorf("monitor alert: unsupported schema %q", env.SchemaID)
	}
	ess := env.Data.Essentials
	props := map[string]string{}
	mergeProps(props, env.Data.CustomProperties)
	mergeProps(props, env.Data.AlertContext.Properties)

	sev := model.Sev3
	if parsed, ok := model.ParseSeverity(ess.Severity); ok {
		sev = parsed
	}
	occurred := received
	if ess.FiredDateTime != "" {
		if t, err := time.Parse(time.RFC3339Nano, ess.FiredDateTime); err == nil {
			occurred = t
		}
	}
	message := firstNonEmpty(props["message"], props["outerMessage"], ess.Description, ess.AlertRule)
	exceptionType := firstNonEmpty(props["exceptionType"], props["exception"], props["problemId"])
	stack := firstNonEmpty(props["stack"], props["stackTrace"], props["details"])
	frame := firstNonEmpty(props["topFrame"], props["frame"], TopFrame(stack))
	operation := firstNonEmpty(props["operation"], props["operation_Name"], props["operationName"])
	errorRate := 0.0
	if raw := firstNonEmpty(props["errorRate"], props["error_rate"]); raw != "" {
		errorRate, _ = strconv.ParseFloat(raw, 64)
	}
	resolved := strings.EqualFold(ess.MonitorCondition, "Resolved")

	targets := ess.AlertTargetIDs
	if len(targets) == 0 {
		targets = []string{""}
	}
	out := make([]model.Signal, 0, len(targets))
	for _, id := range targets {
		sig := model.Signal{
			Source:        model.SourceMonitorAlert,
			ReceivedAt:    received,
			OccurredAt:    occurred,
			Severity:      sev,
			ResourceID:    id,
			ResourceName:  lastSegment(id),
			ResourceType:  classify(id),
			Operation:     operation,
			Message:       message,
			ExceptionType: exceptionType,
			StackTrace:    stack,
			TopFrame:      frame,
			ErrorRate:     errorRate,
			Resolved:      resolved,
			Properties:    cloneMap(props),
		}
		if sig.Properties == nil {
			sig.Properties = map[string]string{}
		}
		if ess.AlertRule != "" {
			sig.Properties["alertRule"] = ess.AlertRule
		}
		if ess.AlertID != "" {
			sig.Properties["alertId"] = ess.AlertID
		}
		for _, cond := range env.Data.AlertContext.Condition.AllOf {
			if cond.MetricValue != nil && sig.Properties["metricValue"] == "" {
				sig.Properties["metricValue"] = strings.TrimSpace(asString(cond.MetricValue))
			}
			if cond.MetricName != "" {
				sig.Properties["metricName"] = cond.MetricName
			}
		}
		out = append(out, sig)
	}
	return out, nil
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeProps(dst map[string]string, src map[string]any) {
	for k, v := range src {
		dst[k] = strings.TrimSpace(asString(v))
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type alertEnvelope struct {
	SchemaID string `json:"schemaId"`
	Data     struct {
		Essentials   essentials `json:"essentials"`
		AlertContext struct {
			Properties map[string]any `json:"properties"`
			Condition  struct {
				AllOf []struct {
					MetricName  string `json:"metricName"`
					MetricValue any    `json:"metricValue"`
					SearchQuery string `json:"searchQuery"`
				} `json:"allOf"`
			} `json:"condition"`
		} `json:"alertContext"`
		CustomProperties map[string]any `json:"customProperties"`
	} `json:"data"`
}

type essentials struct {
	AlertID          string   `json:"alertId"`
	AlertRule        string   `json:"alertRule"`
	Severity         string   `json:"severity"`
	SignalType       string   `json:"signalType"`
	MonitorCondition string   `json:"monitorCondition"`
	AlertTargetIDs   []string `json:"alertTargetIDs"`
	FiredDateTime    string   `json:"firedDateTime"`
	Description      string   `json:"description"`
}
