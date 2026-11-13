package group

import "testing"

func BenchmarkFingerprint(b *testing.B) {
	const (
		res = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-api"
		ex  = "System.NullReferenceException"
		fr  = "OrderService.Place"
		msg = "Object reference not set to an instance of an object. request 48213 failed after 120 ms"
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Fingerprint(res, ex, fr, msg)
	}
}

func BenchmarkGroupSteady(b *testing.B) {
	book := &Book{}
	fps := make([]uint64, 64)
	for i := range fps {
		fps[i] = Fingerprint("res", "Ex", "Frame", "message") + uint64(i)
	}
	for _, fp := range fps {
		book.Add(fp)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		book.Add(fps[i%len(fps)])
	}
}

func BenchmarkGroupParallel(b *testing.B) {
	book := &Book{}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			book.Add(uint64(i % 128))
			i++
		}
	})
}
