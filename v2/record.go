package filemaker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
)

//Record interface for some magic with methods
type Record struct {
	ID            string
	Layout        string
	StagedChanges map[string]interface{}
	FieldData     map[string]interface{}
	Session       *Session
}

//newRecord returns a new instance of an existing record
func newRecord(layout string, data interface{}, session Session) Record {
	return Record{
		ID:            data.(map[string]interface{})["recordId"].(string),
		Layout:        layout,
		StagedChanges: make(map[string]interface{}),
		FieldData:     data.(map[string]interface{})["fieldData"].(map[string]interface{}),
		Session:       &session,
	}
}

//SetField sets the value of a specified field in the given record
func (r *Record) SetField(fieldName string, value interface{}) {
	switch value.(type) {
	case int:
		value = float64(value.(int))
	case int32:
		value = float64(value.(int32))
	case int64:
		value = float64(value.(int64))
	case float32:
		value = float64(value.(float32))
	case float64:
		value = float64(value.(float32))
	case bool:
		if value.(bool) {
			value = float64(1)
		} else {
			value = float64(0)
		}
	}

	r.StagedChanges[fieldName] = value
}

//GetField gets the value of a field in the given record and returns it as an `interface{}`
func (r *Record) GetField(fieldName string) interface{} {
	if val, ok := r.StagedChanges[fieldName]; ok {
		return val
	}

	return r.FieldData[fieldName]
}

//Revert discards all uncommited changes made to the record
func (r *Record) Revert() {
	r.StagedChanges = make(map[string]interface{})
}

//Commit commits the changes made to the record using the same session the record was retrieved/created with
func (r *Record) Commit() error {
	if len(r.StagedChanges) == 0 {
		return nil
	}

	if r.ID == "" {
		return r.Create()
	}

	var fieldData = make(map[string]interface{})

	for fieldName, value := range r.StagedChanges {
		fieldData[fieldName] = value
	}

	var jsonData = struct {
		FieldData map[string]interface{} `json:"fieldData"`
	}{
		fieldData,
	}

	//Create the request json body
	var requestBody, err = json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %v", err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest("PATCH", r.Session.Protocol+r.Session.Host+"/fmi/data/v1/databases/"+r.Session.Database+"/layouts/"+r.Layout+"/records/"+r.ID, bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+r.Session.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send PATCH request: %v", err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return fmt.Errorf("failed to decode response body as json: %v", err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf("failed at host: %v (%v)", jsonRes.Messages[0].Message, jsonRes.Messages[0].Code)
	}

	r.FieldData = fieldData

	return nil
}

//Create inserts the record into the database if it doesn't exist
func (r *Record) Create() error {
	var fieldData = make(map[string]interface{})

	for fieldName, value := range r.StagedChanges {
		fieldData[fieldName] = value
	}

	var jsonData = struct {
		FieldData map[string]interface{} `json:"fieldData"`
	}{
		fieldData,
	}

	//Create the request json body
	var requestBody, err = json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %v", err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest("POST", r.Session.Protocol+r.Session.Host+"/fmi/data/v1/databases/"+r.Session.Database+"/layouts/"+r.Layout+"/records", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+r.Session.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send POST request: %v", err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return fmt.Errorf("failed to decode response body as json: %v", err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf("failed at host: %v (%v)", jsonRes.Messages[0].Message, jsonRes.Messages[0].Code)
	}

	r.ID = jsonRes.Response.RecordID

	return nil
}

//Delete deletes the record using the same session the record was retrieved with
func (r *Record) Delete() error {
	//Build and send request to the host
	req, err := http.NewRequest("DELETE", r.Session.Protocol+r.Session.Host+"/fmi/data/v1/databases/"+r.Session.Database+"/layouts/"+r.Layout+"/records/"+r.ID, bytes.NewBuffer([]byte{}))
	req.Header.Add("Authorization", "Bearer "+r.Session.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send DELETE request: %v", err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read respones body: %v", err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return fmt.Errorf("failed to decode response body as json: %v", err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf("failed at host: %v (%v)", jsonRes.Messages[0].Message, jsonRes.Messages[0].Code)
	}

	//Empty the local record instance
	r.ID = ""
	r.StagedChanges = map[string]interface{}{}
	r.FieldData = map[string]interface{}{}

	return nil
}

/*
String gets the data in the specified field and returns it as a string.
The FileMaker database field needs to be a text field.
*/
func (r *Record) String(fieldName string) (string, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(string); ok {
		return val, nil
	}

	return "", fmt.Errorf("field `%v` value is not of type string: %v", fieldName, data)
}

/*
Int gets the data in the specified field and returns it as an int.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Int(fieldName string) (int, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return int(val), nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type int: %v", fieldName, data)
}

/*
Int32 gets the data in the specified field and returns it as an int32.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Int32(fieldName string) (int32, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return int32(val), nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type int32: %v", fieldName, data)
}

/*
Int64 gets the data in the specified field and returns it as an int64.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Int64(fieldName string) (int64, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return int64(val), nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type int64: %v", fieldName, data)
}

/*
Float32 gets the data in the specified field and returns it as an float32.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Float32(fieldName string) (float32, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return float32(val), nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type float32: %v", fieldName, data)
}

/*
Float64 gets the data in the specified field and returns it as an float64.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Float64(fieldName string) (float64, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return val, nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type float64: %v", fieldName, data)
}

/*
Bool gets the data in the specified field and returns it as an bool.
The FileMaker database field needs to be a number field. Numbers
larger than `0` return `true` and `0` or below returns `false`.
*/
func (r *Record) Bool(fieldName string) (bool, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		if val > 0 {
			return true, nil
		}
		return false, nil
	}

	return false, fmt.Errorf("field `%v` value is not of type bool: %v", fieldName, data)
}

/*
Map takes a struct and inserts the field data of the record
in the struct fields with an `fm`-tag matching the record field name.

Example struct:
`
type example struct {
	Name string `fm:"Name"`
	Age int `fm:"Age"`
}
`

- A pointer to the object must be passed (i.e `Record.Map(&obj)`).

- Nested structs are not supported.

Supported types:

- string

- int

- int64

- float64

- bool
*/
func (r *Record) Map(obj interface{}) error {
	v := reflect.Indirect(reflect.ValueOf(obj))
	typeOfObj := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)

		if !field.IsValid() || !field.CanSet() {
			continue
		}

		tag := typeOfObj.Field(i).Tag.Get("fm")

		switch field.Interface().(type) {
		case string:
			val, err := r.String(tag)
			if err != nil {
				return err
			}
			field.SetString(val)
		case int, int64:
			val, err := r.Int64(tag)
			if err != nil {
				return err
			}
			field.SetInt(val)
		case float64:
			val, err := r.Float64(tag)
			if err != nil {
				return err
			}
			field.SetFloat(val)
		case bool:
			val, err := r.Bool(tag)
			if err != nil {
				return err
			}
			field.SetBool(val)
		}
	}

	return nil
}
