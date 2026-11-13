// Package azure calls Azure with DefaultAzureCredential. Secrets are not read
// from source; the credential chain uses managed identity, workload identity,
// or the developer CLI session.
package azure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azlogs"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appcontainers/armappcontainers/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v5"
	"github.com/larryjoelane/healbot/internal/azureid"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/platform"
)

// Runtime is the Azure SDK implementation of platform.Runtime.
type Runtime struct {
	web       *armappservice.WebAppsClient
	plans     *armappservice.PlansClient
	apps      *armappcontainers.ContainerAppsClient
	revs      *armappcontainers.ContainerAppsRevisionsClient
	logs      *azlogs.Client
	workspace string
}

// NewRuntime builds clients with the provided credential. Pass the credential
// from NewCredential in production.
func NewRuntime(subscriptionID string, cred azcore.TokenCredential, workspaceID string, opts *arm.ClientOptions) (*Runtime, error) {
	if subscriptionID == "" {
		return nil, errors.New("azure: subscription id is required")
	}
	if cred == nil {
		return nil, errors.New("azure: credential is required")
	}
	web, err := armappservice.NewWebAppsClient(subscriptionID, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: web apps client: %w", err)
	}
	plans, err := armappservice.NewPlansClient(subscriptionID, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: plans client: %w", err)
	}
	apps, err := armappcontainers.NewContainerAppsClient(subscriptionID, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: container apps client: %w", err)
	}
	revs, err := armappcontainers.NewContainerAppsRevisionsClient(subscriptionID, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azure: revisions client: %w", err)
	}
	var logsClient *azlogs.Client
	if workspaceID != "" {
		var logOpts *azlogs.ClientOptions
		if opts != nil {
			logOpts = &azlogs.ClientOptions{ClientOptions: opts.ClientOptions}
		}
		logsClient, err = azlogs.NewClient(cred, logOpts)
		if err != nil {
			return nil, fmt.Errorf("azure: logs client: %w", err)
		}
	}
	return &Runtime{web: web, plans: plans, apps: apps, revs: revs, logs: logsClient, workspace: workspaceID}, nil
}

// NewCredential returns DefaultAzureCredential. Managed identity is used when
// the process runs in Azure. No client secret is read here.
func NewCredential() (*azidentity.DefaultAzureCredential, error) {
	return azidentity.NewDefaultAzureCredential(nil)
}

// Restart restarts an App Service or Function App. Both are Microsoft.Web/sites.
func (r *Runtime) Restart(ctx context.Context, resourceID string) error {
	ref, err := azureid.Parse(resourceID)
	if err != nil {
		return err
	}
	if ref.ResourceType() != model.TypeWebSite {
		return fmt.Errorf("restart: %s is not a web or function app", resourceID)
	}
	_, err = r.web.Restart(ctx, ref.Group, ref.Name, nil)
	return err
}

// SwapSlot swaps production with the named slot. The slot should hold the last known-good build.
func (r *Runtime) SwapSlot(ctx context.Context, resourceID, slot string) error {
	ref, err := azureid.Parse(resourceID)
	if err != nil {
		return err
	}
	preserve := false
	poller, err := r.web.BeginSwapSlotWithProduction(ctx, ref.Group, ref.Name, armappservice.CsmSlotEntity{
		TargetSlot:   &slot,
		PreserveVnet: &preserve,
	}, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// ScaleOut adds instances, stopping at maxInstances.
func (r *Runtime) ScaleOut(ctx context.Context, resourceID string, delta, maxInstances int32) error {
	ref, err := azureid.Parse(resourceID)
	if err != nil {
		return err
	}
	if delta < 1 {
		delta = 1
	}
	if ref.ResourceType() == model.TypeContainerApp {
		return r.scaleContainer(ctx, ref, delta, maxInstances)
	}
	if ref.ResourceType() != model.TypeWebSite {
		return fmt.Errorf("scale out: unsupported resource %s", resourceID)
	}
	site, err := r.web.Get(ctx, ref.Group, ref.Name, nil)
	if err != nil {
		return err
	}
	if site.Properties == nil || site.Properties.ServerFarmID == nil {
		return errors.New("scale out: site has no app service plan")
	}
	plan, err := azureid.Parse(*site.Properties.ServerFarmID)
	if err != nil {
		return err
	}
	got, err := r.plans.Get(ctx, plan.Group, plan.Name, nil)
	if err != nil {
		return err
	}
	if got.SKU == nil || got.SKU.Capacity == nil {
		return errors.New("scale out: plan has no sku capacity")
	}
	next := *got.SKU.Capacity + delta
	if maxInstances > 0 && next > maxInstances {
		if *got.SKU.Capacity >= maxInstances {
			return platform.ErrAtCapacity
		}
		next = maxInstances
	}
	got.SKU.Capacity = &next
	if got.Location == nil {
		return errors.New("scale out: plan has no location")
	}
	poller, err := r.plans.BeginCreateOrUpdate(ctx, plan.Group, plan.Name, got.Plan, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (r *Runtime) scaleContainer(ctx context.Context, ref azureid.Ref, delta, maxInstances int32) error {
	got, err := r.apps.Get(ctx, ref.Group, ref.Name, nil)
	if err != nil {
		return err
	}
	min := int32(1)
	max := int32(1)
	if got.Properties != nil && got.Properties.Template != nil && got.Properties.Template.Scale != nil {
		if got.Properties.Template.Scale.MinReplicas != nil {
			min = *got.Properties.Template.Scale.MinReplicas
		}
		if got.Properties.Template.Scale.MaxReplicas != nil {
			max = *got.Properties.Template.Scale.MaxReplicas
		}
	}
	min += delta
	if max < min {
		max = min
	}
	if maxInstances > 0 && min > maxInstances {
		return platform.ErrAtCapacity
	}
	patch := armappcontainers.ContainerApp{
		Location: got.Location,
		Properties: &armappcontainers.ContainerAppProperties{
			Template: &armappcontainers.Template{
				Scale: &armappcontainers.Scale{MinReplicas: &min, MaxReplicas: &max},
			},
		},
	}
	poller, err := r.apps.BeginUpdate(ctx, ref.Group, ref.Name, patch, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// RestartRevision restarts one container app revision. An empty name restarts the latest revision.
func (r *Runtime) RestartRevision(ctx context.Context, resourceID, revision string) error {
	ref, err := azureid.Parse(resourceID)
	if err != nil {
		return err
	}
	if revision == "" {
		revision, err = r.latestRevision(ctx, ref)
		if err != nil {
			return err
		}
	}
	_, err = r.revs.RestartRevision(ctx, ref.Group, ref.Name, revision, nil)
	return err
}

// RollbackRevision activates the named revision and, when ingress traffic is
// already split, points all of it at that revision. An empty name uses the
// newest revision that is not the current latest.
func (r *Runtime) RollbackRevision(ctx context.Context, resourceID, revision string) error {
	ref, err := azureid.Parse(resourceID)
	if err != nil {
		return err
	}
	if revision == "" {
		revision, err = r.previousRevision(ctx, ref)
		if err != nil {
			return err
		}
	}
	if _, err = r.revs.ActivateRevision(ctx, ref.Group, ref.Name, revision, nil); err != nil {
		return err
	}
	got, err := r.apps.Get(ctx, ref.Group, ref.Name, nil)
	if err != nil {
		return err
	}
	if got.Properties == nil || got.Properties.Configuration == nil || got.Properties.Configuration.Ingress == nil {
		return nil
	}
	ingress := got.Properties.Configuration.Ingress
	if len(ingress.Traffic) == 0 {
		return nil
	}
	weight := int32(100)
	latest := false
	patch := armappcontainers.ContainerApp{
		Location: got.Location,
		Properties: &armappcontainers.ContainerAppProperties{
			Configuration: &armappcontainers.Configuration{
				Ingress: &armappcontainers.Ingress{
					Traffic: []*armappcontainers.TrafficWeight{{
						RevisionName:   &revision,
						Weight:         &weight,
						LatestRevision: &latest,
					}},
				},
			},
		},
	}
	poller, err := r.apps.BeginUpdate(ctx, ref.Group, ref.Name, patch, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// ListChanges returns recent deployments or revisions, newest first when the API provides times.
func (r *Runtime) ListChanges(ctx context.Context, resourceID string) ([]model.Deployment, error) {
	ref, err := azureid.Parse(resourceID)
	if err != nil {
		return nil, err
	}
	switch ref.ResourceType() {
	case model.TypeWebSite:
		return r.listDeployments(ctx, ref)
	case model.TypeContainerApp:
		return r.listRevisions(ctx, ref)
	default:
		return nil, nil
	}
}

func (r *Runtime) listDeployments(ctx context.Context, ref azureid.Ref) ([]model.Deployment, error) {
	pager := r.web.NewListDeploymentsPager(ref.Group, ref.Name, nil)
	var out []model.Deployment
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return out, err
		}
		for _, d := range page.Value {
			if d == nil {
				continue
			}
			item := model.Deployment{Name: deref(d.Name), Active: d.Properties != nil && d.Properties.Active != nil && *d.Properties.Active}
			if d.Properties != nil && d.Properties.StartTime != nil {
				item.At = *d.Properties.StartTime
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *Runtime) listRevisions(ctx context.Context, ref azureid.Ref) ([]model.Deployment, error) {
	pager := r.revs.NewListRevisionsPager(ref.Group, ref.Name, nil)
	var out []model.Deployment
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return out, err
		}
		for _, rev := range page.Value {
			if rev == nil {
				continue
			}
			item := model.Deployment{Name: deref(rev.Name), Revision: deref(rev.Name)}
			if rev.Properties != nil {
				if rev.Properties.CreatedTime != nil {
					item.At = *rev.Properties.CreatedTime
				}
				item.Active = rev.Properties.Active != nil && *rev.Properties.Active
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// Health reports container revision health. App Service health is intentionally
// not inferred: set a health_url so a private site is not probed by guesswork.
func (r *Runtime) Health(ctx context.Context, resourceID string) (model.HealthStatus, error) {
	ref, err := azureid.Parse(resourceID)
	if err != nil {
		return model.HealthStatus{}, err
	}
	if ref.ResourceType() != model.TypeContainerApp {
		return model.HealthStatus{}, errors.New("no health signal configured for this resource; set health_url")
	}
	name, err := r.latestRevision(ctx, ref)
	if err != nil {
		return model.HealthStatus{}, err
	}
	got, err := r.revs.GetRevision(ctx, ref.Group, ref.Name, name, nil)
	if err != nil {
		return model.HealthStatus{}, err
	}
	if got.Properties == nil || got.Properties.HealthState == nil {
		return model.HealthStatus{}, errors.New("revision has no health state")
	}
	state := string(*got.Properties.HealthState)
	return model.HealthStatus{Healthy: strings.EqualFold(state, "Healthy"), Detail: "revision " + name + " is " + state}, nil
}

// QuerySignals runs a KQL query and maps rows into signals.
func (r *Runtime) QuerySignals(ctx context.Context, kql string) ([]model.Signal, error) {
	if r.logs == nil || r.workspace == "" {
		return nil, errors.New("log analytics workspace is not configured")
	}
	end := time.Now().UTC()
	start := end.Add(-15 * time.Minute)
	resp, err := r.logs.QueryWorkspace(ctx, r.workspace, azlogs.QueryBody{
		Query:    &kql,
		Timespan: to.Ptr(azlogs.NewTimeInterval(start, end)),
	}, nil)
	if err != nil {
		return nil, err
	}
	var out []model.Signal
	for _, table := range resp.Tables {
		idx := map[string]int{}
		for i, col := range table.Columns {
			if col.Name != nil {
				idx[strings.ToLower(*col.Name)] = i
			}
		}
		for _, row := range table.Rows {
			sig := model.Signal{Source: model.SourceLogAnalytics, Severity: model.Sev3}
			sig.ExceptionType = cell(row, idx, "problemid", "exceptiontype", "type")
			sig.Message = cell(row, idx, "outermessage", "innermostmessage", "message")
			sig.Operation = cell(row, idx, "operation_name", "operationname")
			sig.ResourceID = cell(row, idx, "resourceid")
			sig.StackTrace = cell(row, idx, "details", "stack")
			if ts := cell(row, idx, "timestamp", "timegenerated"); ts != "" {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
					sig.OccurredAt = t
				}
			}
			if sig.ResourceID != "" {
				if parsed, err := azureid.Parse(sig.ResourceID); err == nil {
					sig.ResourceName = parsed.Name
					sig.ResourceType = parsed.ResourceType()
				}
			}
			out = append(out, sig)
		}
	}
	return out, nil
}

func (r *Runtime) latestRevision(ctx context.Context, ref azureid.Ref) (string, error) {
	got, err := r.apps.Get(ctx, ref.Group, ref.Name, nil)
	if err != nil {
		return "", err
	}
	if got.Properties != nil && got.Properties.LatestReadyRevisionName != nil && *got.Properties.LatestReadyRevisionName != "" {
		return *got.Properties.LatestReadyRevisionName, nil
	}
	if got.Properties != nil && got.Properties.LatestRevisionName != nil {
		return *got.Properties.LatestRevisionName, nil
	}
	return "", errors.New("container app has no revision name")
}

func (r *Runtime) previousRevision(ctx context.Context, ref azureid.Ref) (string, error) {
	revs, err := r.listRevisions(ctx, ref)
	if err != nil {
		return "", err
	}
	latest, _ := r.latestRevision(ctx, ref)
	for _, rev := range revs {
		if rev.Revision != "" && rev.Revision != latest {
			return rev.Revision, nil
		}
	}
	return "", errors.New("no previous container revision to roll back to")
}

func cell(row azlogs.Row, idx map[string]int, names ...string) string {
	for _, name := range names {
		i, ok := idx[name]
		if !ok || i >= len(row) || row[i] == nil {
			continue
		}
		return strings.TrimSpace(fmt.Sprint(row[i]))
	}
	return ""
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
