package formatter

import (
	"fmt"
	"strings"
)

// CompositeKind describes the proven underlying type, independently of whether
// its elements are keyed or the user has selected it for expansion.
type CompositeKind uint8

const (
	CompositeKindUnknown CompositeKind = iota
	CompositeKindStruct
	CompositeKindMap
	CompositeKindSlice
	CompositeKindArray
)

func ParseExpand(value string) (map[CompositeKind]bool, error) {
	kinds := make(map[CompositeKind]bool)
	for _, part := range strings.Split(value, ",") {
		var kind CompositeKind
		switch strings.TrimSpace(part) {
		case "struct":
			kind = CompositeKindStruct
		case "map":
			kind = CompositeKindMap
		default:
			return nil, fmt.Errorf("unsupported --expand kind %q: supported kinds are struct,map", part)
		}
		if kinds[kind] {
			return nil, fmt.Errorf("duplicate --expand kind %q", strings.TrimSpace(part))
		}
		kinds[kind] = true
	}
	return kinds, nil
}
