// Package group fingerprints error signals so repeats collapse into one incident.
// The hot path is allocation-free after the buffer pool warms up.
package group

import (
	"sync"

	"github.com/cespare/xxhash/v2"
)

var bufPool = sync.Pool{New: func() any {
	b := make([]byte, 0, 256)
	return &b
}}

// Fingerprint mixes the resource, exception, top frame, and a normalized
// message. Digits collapse to '#' so "failed 12 times" and "failed 99 times"
// land in the same incident. Resource IDs are compared case-insensitively.
func Fingerprint(resource, exception, frame, message string) uint64 {
	p := bufPool.Get().(*[]byte)
	buf := (*p)[:0]
	buf = appendLower(buf, resource)
	buf = append(buf, 0)
	buf = append(buf, exception...)
	buf = append(buf, 0)
	buf = append(buf, frame...)
	buf = append(buf, 0)
	buf = appendNormalized(buf, message)
	sum := xxhash.Sum64(buf)
	*p = buf
	bufPool.Put(p)
	return sum
}

func appendLower(dst []byte, src string) []byte {
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		dst = append(dst, c)
	}
	return dst
}

func appendNormalized(dst []byte, src string) []byte {
	space := true
	digit := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c >= '0' && c <= '9':
			if !digit {
				dst = append(dst, '#')
				digit = true
			}
			space = false
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			digit = false
			if !space {
				dst = append(dst, ' ')
				space = true
			}
		default:
			digit = false
			space = false
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			dst = append(dst, c)
		}
	}
	if len(dst) > 0 && dst[len(dst)-1] == ' ' {
		dst = dst[:len(dst)-1]
	}
	return dst
}
