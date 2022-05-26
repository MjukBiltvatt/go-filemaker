package filemaker

import (
	"testing"
	"time"
)

func newTestRecord() Record {
	return newRecord(
		"layout",
		map[string]interface{}{
			"recordId": "recordId",
			"fieldData": map[string]interface{}{
				"string":              "string",
				"int":                 float64(100),
				"int8":                float64(8),
				"int16":               float64(16),
				"int32":               float64(32),
				"int64":               float64(64),
				"float32":             float64(32.32),
				"float64":             float64(64.64),
				"bool_true_txt_test":  "test",
				"bool_true_txt_false": "false",
				"bool_false_txt":      "",
				"bool_true_num_1":     float64(1),
				"bool_true_num_123":   float64(123),
				"bool_false_num_0":    float64(0),
				"date":                "01/02/2006",
				"timestamp":           "01/02/2006 15:04:05",
			},
		},
		Session{
			Token:    "token",
			Host:     "host",
			Database: "database",
			Username: "username",
			Password: "password",
		},
	)
}

type testRecordStruct struct {
	String           string    `fm:"string"`
	Int              int       `fm:"int"`
	Int8             int8      `fm:"int8"`
	Int16            int16     `fm:"int16"`
	Int32            int32     `fm:"int32"`
	Int64            int64     `fm:"int64"`
	Float32          float32   `fm:"float32"`
	Float64          float64   `fm:"float64"`
	BoolTrueTxtTest  bool      `fm:"bool_true_txt_test"`
	BoolTrueTxtFalse bool      `fm:"bool_true_txt_false"`
	BoolFalseTxt     bool      `fm:"bool_false_txt"`
	BoolTrueNum1     bool      `fm:"bool_true_num_1"`
	BoolTrueNum123   bool      `fm:"bool_true_num_123"`
	BoolFalseNum0    bool      `fm:"bool_false_num_0"`
	Date             time.Time `fm:"date"`
	DateInvalid      time.Time `fm:"date_invalid"`
	Timestamp        time.Time `fm:"timestamp"`
	TimestampInvalid time.Time `fm:"timestamp_invalid"`
}

//TestRecordMap tests the `Record.Map` method
func TestRecordMap(t *testing.T) {
	//Create a dummy record
	record := newTestRecord()

	//Create a struct to map the dummy record to
	var value testRecordStruct

	//Map the dummy record to the struct
	record.Map(&value)

	t.Run("string", func(t *testing.T) {
		got := value.String
		expect := "string"
		if got != expect {
			t.Errorf("got: '%v', expected: '%v'", got, expect)
		}
	})

	t.Run("int", func(t *testing.T) {
		got := value.Int
		var expect int = 100
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("int8", func(t *testing.T) {
		got := value.Int8
		var expect int8 = 8
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("int16", func(t *testing.T) {
		got := value.Int16
		var expect int16 = 16
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("int32", func(t *testing.T) {
		got := value.Int32
		var expect int32 = 32
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("int64", func(t *testing.T) {
		got := value.Int64
		var expect int64 = 64
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("float32", func(t *testing.T) {
		got := value.Float32
		var expect float32 = 32.32
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("float64", func(t *testing.T) {
		got := value.Float64
		var expect float64 = 64.64
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("bool_true_txt_test", func(t *testing.T) {
		got := value.BoolTrueTxtTest
		expect := true
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("bool_true_txt_false", func(t *testing.T) {
		got := value.BoolTrueTxtFalse
		expect := true
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("bool_false_txt", func(t *testing.T) {
		got := value.BoolFalseTxt
		expect := false
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("bool_true_num_1", func(t *testing.T) {
		got := value.BoolTrueNum1
		expect := true
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("bool_true_num_123", func(t *testing.T) {
		got := value.BoolTrueNum123
		expect := true
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("bool_false_num_0", func(t *testing.T) {
		got := value.BoolFalseNum0
		expect := false
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("date", func(t *testing.T) {
		got := value.Date
		expect := time.Date(2006, 1, 2, 0, 0, 0, 0, time.Local)
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("date_invalid", func(t *testing.T) {
		got := value.DateInvalid
		expect := time.Time{}
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("timestamp", func(t *testing.T) {
		got := value.Timestamp
		expect := time.Date(2006, 1, 2, 15, 4, 5, 0, time.Local)
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("timestamp_invalid", func(t *testing.T) {
		got := value.TimestampInvalid
		expect := time.Time{}
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})
}
