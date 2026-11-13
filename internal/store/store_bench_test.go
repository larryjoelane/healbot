package store

import (
	"testing"

	"github.com/larryjoelane/healbot/internal/model"
)

func BenchmarkAbsorbSteady(b *testing.B) {
	s := New(nil)
	sig := &model.Signal{
		Severity: model.Sev2, ResourceID: "res", ExceptionType: "Ex",
		TopFrame: "Frame", Message: "failed 12 times",
	}
	s.Absorb(sig)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Absorb(sig)
	}
}
