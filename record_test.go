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
				"string_2":            "string2",
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
				"date_1":              "01/02/2006",
				"date_2":              "2006-01-02",
				"timestamp_1":         "01/02/2006 15:04:05",
				"timestamp_2":         "2006-01-02 15:04:05",
				"time_invalid":        "january 1 2006 15 pm",
				"related::string":     "related",
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

type nestedStructPointer struct {
	String string `fm:"string"`
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
	Date1            time.Time `fm:"date_1"`
	Date2            time.Time `fm:"date_2"`
	Timestamp1       time.Time `fm:"timestamp_1"`
	Timestamp2       time.Time `fm:"timestamp_2"`
	TimeInvalid      time.Time `fm:"time_invalid"`
	Nested           struct {
		String string `fm:"related::string"`
		Nested struct {
			String string `fm:"string_2"`
		}
	}
	NestedStructPointer    *nestedStructPointer
	NestedNilStructPointer *nestedStructPointer
}

//TestRecordMap tests the `Record.Map` method
func TestRecordMap(t *testing.T) {
	//Create a dummy record
	record := newTestRecord()

	//Create a struct to map the dummy record to
	var value testRecordStruct
	value.NestedStructPointer = &nestedStructPointer{}

	//Map the dummy record to the struct
	record.Map(&value, time.UTC)

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

	t.Run("date_1", func(t *testing.T) {
		got := value.Date1
		expect := time.Date(2006, 1, 2, 0, 0, 0, 0, time.UTC)
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("date_2", func(t *testing.T) {
		got := value.Date2
		expect := time.Date(2006, 1, 2, 0, 0, 0, 0, time.UTC)
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("timestamp_1", func(t *testing.T) {
		got := value.Timestamp1
		expect := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("timestamp_2", func(t *testing.T) {
		got := value.Timestamp2
		expect := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("time_invalid", func(t *testing.T) {
		got := value.TimeInvalid
		expect := time.Time{}
		if got != expect {
			t.Errorf("got: %v, expected: %v", got, expect)
		}
	})

	t.Run("nested_related_string", func(t *testing.T) {
		got := value.Nested.String
		expect := "related"
		if got != expect {
			t.Errorf("got: '%v', expected: '%v'", got, expect)
		}
	})

	t.Run("nested_nested_string", func(t *testing.T) {
		got := value.Nested.Nested.String
		expect := "string2"
		if got != expect {
			t.Errorf("got: '%v', expected: '%v'", got, expect)
		}
	})

	t.Run("nested_pointer_string", func(t *testing.T) {
		got := value.NestedStructPointer.String
		expect := "string"
		if got != expect {
			t.Errorf("got: '%v', expected: '%v'", got, expect)
		}
	})

	t.Run("nested_nil_pointer", func(t *testing.T) {
		got := value.NestedNilStructPointer
		if got != nil {
			t.Errorf("got: %v, expected: %v", got, nil)
		}
	})
}
