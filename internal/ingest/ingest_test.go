package ingest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
)

const sampleLine = "2026-11-13T14:02:01.123Z|2|/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-api|System.NullReferenceException|POST /v1/orders|OrderService.Place|Object reference not set to an instance of an object."

func TestParsePipeCopiesBytes(t *testing.T) {
	line := []byte(sampleLine)
	var sig model.Signal
	if err := ParseLogLine(line, &sig); err != nil {
		t.Fatal(err)
	}
	for i := range line {
		line[i] = 'X'
	}
	if sig.ExceptionType != "System.NullReferenceException" {
		t.Fatal(sig.ExceptionType)
	}
	if sig.TopFrame != "OrderService.Place" {
		t.Fatal(sig.TopFrame)
	}
	if sig.Severity != model.Sev2 {
		t.Fatal(sig.Severity)
	}
	if sig.ResourceType != model.TypeWebSite {
		t.Fatal(sig.ResourceType)
	}
	if sig.ResourceName != "example-api" {
		t.Fatal(sig.ResourceName)
	}
	if !sig.OccurredAt.Equal(time.Date(2026, 11, 13, 14, 2, 1, 123000000, time.UTC)) {
		t.Fatal(sig.OccurredAt)
	}
	if !strings.Contains(sig.Message, "Object reference") {
		t.Fatal(sig.Message)
	}
}

func TestParsePipeRejectsShort(t *testing.T) {
	if err := ParseLogLine([]byte("only|two"), &model.Signal{}); !errors.Is(err, errShape) {
		t.Fatal(err)
	}
}

func TestParseLogJSON(t *testing.T) {
	line := []byte(`{"timestamp":"2026-11-13T14:02:01Z","severity":"sev1","resourceId":"/subscriptions/s/resourceGroups/rg/providers/Microsoft.App/containerApps/shop","problemId":"System.TimeoutException","outerMessage":"dependency timed out","operation_Name":"GET /health","stack":"at Shop.Pay.Charge(Pay.cs:12)"}`)
	var sig model.Signal
	if err := ParseLogLine(line, &sig); err != nil {
		t.Fatal(err)
	}
	if sig.ResourceType != model.TypeContainerApp || sig.Severity != model.Sev1 {
		t.Fatalf("%+v", sig)
	}
	if sig.TopFrame != "Pay.Charge" {
		t.Fatal(sig.TopFrame)
	}
}

func TestParseStream(t *testing.T) {
	body := sampleLine + "\n\n" + sampleLine + "\n"
	n := 0
	err := ParseLogStream(strings.NewReader(body), func(s *model.Signal) error {
		n++
		if s.ExceptionType == "" {
			t.Fatal("missing exception")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatal(n)
	}
}

func TestTopFrame(t *testing.T) {
	stack := "at Example.Orders.OrderService.Place(OrderService.cs:88)\nat Example.Orders.OrdersController.Post(OrdersController.cs:41)"
	if got := TopFrame(stack); got != "OrderService.Place" {
		t.Fatal(got)
	}
}

func TestMonitorAlert(t *testing.T) {
	body := []byte(`{
	  "schemaId": "azureMonitorCommonAlertSchema",
	  "data": {
	    "essentials": {
	      "alertId": "/subscriptions/sub/alerts/1",
	      "alertRule": "example-api failed requests",
	      "severity": "Sev2",
	      "monitorCondition": "Fired",
	      "alertTargetIDs": ["/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-api"],
	      "firedDateTime": "2026-11-13T14:02:01.123Z",
	      "description": "Failed requests spiked after the production slot swap."
	    },
	    "alertContext": {
	      "properties": {
	        "exceptionType": "System.NullReferenceException",
	        "message": "Object reference not set to an instance of an object.",
	        "operation": "POST /v1/orders",
	        "topFrame": "OrderService.Place",
	        "stack": "at Example.Orders.OrderService.Place(OrderService.cs:88)",
	        "errorRate": "0.184"
	      },
	      "condition": {"allOf": [{"metricName": "Http5xx", "metricValue": 42}]}
	    }
	  }
	}`)
	sigs, err := ParseMonitorAlert(bytes.NewReader(body), time.Date(2026, 11, 13, 14, 3, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(sigs) != 1 {
		t.Fatal(len(sigs))
	}
	sig := sigs[0]
	if sig.Severity != model.Sev2 || sig.ErrorRate != 0.184 || sig.TopFrame != "OrderService.Place" {
		t.Fatalf("%+v", sig)
	}
	if sig.Properties["metricValue"] != "42" || sig.Properties["alertRule"] == "" {
		t.Fatalf("%v", sig.Properties)
	}
}

func TestMonitorResolved(t *testing.T) {
	body := []byte(`{"schemaId":"azureMonitorCommonAlertSchema","data":{"essentials":{"severity":"Sev3","monitorCondition":"Resolved","alertTargetIDs":["/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/api"],"description":"cleared"}}}`)
	sigs, err := ParseMonitorAlert(bytes.NewReader(body), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !sigs[0].Resolved {
		t.Fatal("expected resolved")
	}
}

func TestPoolBackpressure(t *testing.T) {
	p := NewPool(1, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := p.Submit(context.Background(), func() {
		close(started)
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := p.TrySubmit(func() {}); err != nil {
		t.Fatal(err)
	}
	if err := p.TrySubmit(func() {}); !errors.Is(err, ErrSaturated) {
		t.Fatalf("got %v", err)
	}
	close(release)
	p.Close()
}

func TestPoolProcessesAll(t *testing.T) {
	p := NewPool(4, 8)
	var n sync.WaitGroup
	var mu sync.Mutex
	got := 0
	for i := 0; i < 100; i++ {
		n.Add(1)
		if err := p.Submit(context.Background(), func() {
			mu.Lock()
			got++
			mu.Unlock()
			n.Done()
		}); err != nil {
			t.Fatal(err)
		}
	}
	n.Wait()
	p.Close()
	if got != 100 {
		t.Fatal(got)
	}
}

func TestPoolSubmitCanceled(t *testing.T) {
	p := NewPool(1, 1)
	started := make(chan struct{})
	hold := make(chan struct{})
	if err := p.Submit(context.Background(), func() {
		close(started)
		<-hold
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := p.TrySubmit(func() {}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.Submit(ctx, func() {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	close(hold)
	p.Close()
}
