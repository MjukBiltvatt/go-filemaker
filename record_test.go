package filemaker

import "testing"

func TestRecord(t *testing.T) {
	record := newRecord(
		"layout",
		map[string]interface{}{
			"string":    "string",
			"int":       int(1),
			"int8":      int8(8),
			"int16":     int16(16),
			"int32":     int32(32),
			"int64":     int64(64),
			"float32":   float32(32.32),
			"float64":   float64(64.64),
			"date":      "01/02/2006",
			"timestamp": "01/02/2006 15:04:05",
		},
		Session{},
	)

}
