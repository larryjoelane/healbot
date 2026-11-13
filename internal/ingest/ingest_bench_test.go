package ingest

import (
	"bytes"
	"testing"

	"github.com/larryjoelane/healbot/internal/model"
)

func BenchmarkParseLogLine(b *testing.B) {
	line := []byte(sampleLine)
	var sig model.Signal
	b.ReportAllocs()
	b.SetBytes(int64(len(line)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ParseLogLine(line, &sig); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseLogStream(b *testing.B) {
	body := make([]byte, 0, (len(sampleLine)+1)*64)
	for i := 0; i < 64; i++ {
		body = append(body, sampleLine...)
		body = append(body, '\n')
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := 0
		err := ParseLogStream(bytes.NewReader(body), func(*model.Signal) error {
			n++
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
		if n != 64 {
			b.Fatalf("parsed %d", n)
		}
	}
}
