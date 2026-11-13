package store

import (
	"sync"
	"testing"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
)

func TestAbsorbGroups(t *testing.T) {
	s := New(func() time.Time { return time.Date(2026, 11, 13, 14, 0, 0, 0, time.UTC) })
	sig := &model.Signal{
		ID: "s1", Source: model.SourceLogBatch, Severity: model.Sev2,
		ResourceID:   "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/api",
		ResourceName: "api", ResourceType: model.TypeWebSite,
		ExceptionType: "System.NullReferenceException", TopFrame: "OrderService.Place",
		Message: "failed 12 times",
	}
	a := s.Absorb(sig)
	if !a.Created || a.Incident.Count != 1 {
		t.Fatalf("%+v", a)
	}
	sig2 := *sig
	sig2.ID = "s2"
	sig2.Message = "failed 40 times"
	sig2.Severity = model.Sev1
	b := s.Absorb(&sig2)
	if b.Created || b.Incident.Count != 2 || !b.SeverityRaised || b.Incident.ID != a.Incident.ID {
		t.Fatalf("%+v", b.Incident)
	}
	if b.Incident.Severity != model.Sev1 {
		t.Fatal(b.Incident.Severity)
	}
}

func TestParallelAbsorb(t *testing.T) {
	s := New(nil)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s.Absorb(&model.Signal{
					Severity:      model.Sev2,
					ResourceID:    "res",
					ExceptionType: "Ex",
					TopFrame:      "Frame",
					Message:       "boom",
				})
			}
		}(g)
	}
	wg.Wait()
	list := s.List()
	if len(list) != 1 || list[0].Count != 400 {
		t.Fatalf("%+v", list)
	}
}
