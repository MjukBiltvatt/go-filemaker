package filemaker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"time"
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

//Set sets the value of a specified field in the given record
func (r *Record) Set(fieldName string, value interface{}) {
	switch value.(type) {
	case int:
		value = float64(value.(int))
	case int8:
		value = float64(value.(int8))
	case int16:
		value = float64(value.(int16))
	case int32:
		value = float64(value.(int32))
	case int64:
		value = float64(value.(int64))
	case float32:
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

//Get gets the value of a field in the given record and returns it as an `interface{}`
func (r *Record) Get(fieldName string) interface{} {
	if val, ok := r.StagedChanges[fieldName]; ok {
		return val
	}

	return r.FieldData[fieldName]
}

//Reset discards all uncommited changes made to the record
func (r *Record) Reset() {
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

	var jsonData = struct {
		FieldData map[string]interface{} `json:"fieldData"`
	}{
		r.StagedChanges,
	}

	//Create the request json body
	var requestBody, err = json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %v", err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest(
		"PATCH",
		r.Session.recordsURL(r.Layout, r.ID),
		bytes.NewBuffer(requestBody),
	)
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
		return fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
	}

	for fieldName, value := range r.StagedChanges {
		r.FieldData[fieldName] = value
	}

	return nil
}

//CommitToContainer commits the specified bytes buffer to the specified container field in the record.
func (r *Record) CommitToContainer(fieldName, filename string, dataBuf bytes.Buffer) error {
	if r.ID == "" {
		return errors.New("Record needs to be created first")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, err := writer.CreateFormFile("upload", filename)
	if err != nil {
		return errors.New("failed to write to field 'upload'")
	}

	//Build multipart/form-data header for request
	if _, err := io.Copy(fw, &dataBuf); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	//Build and send request to the host
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf(
			"%s/containers/%s",
			r.Session.recordsURL(r.Layout, r.ID),
			fieldName,
		),
		body,
	)
	cd := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	req.Header.Set("Content-Disposition", cd)
	req.Header.Set("Content-Type", writer.FormDataContentType())
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

	// //Unmarshal json body
	jsonRes := &ResponseBody{}
	if err := json.Unmarshal(resBodyBytes, &jsonRes); err != nil {
		return fmt.Errorf("failed to decode response body as json: %v", err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
	}

	return nil
}

//CommitFileToContainer commits the specified file to specified container field in the record
func (r *Record) CommitFileToContainer(fieldName, filepath string) error {
	//Record is empty and not created yet
	if r.ID == "" {
		return errors.New("record needs to be created first")
	}

	b, err := ioutil.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}
	buf := bytes.NewBuffer(b)

	pathSlice := strings.Split(filepath, "/")
	filename := pathSlice[len(pathSlice)-1]

	return r.CommitToContainer(fieldName, filename, *buf)
}

//Create inserts the record into the database if it doesn't exist
func (r *Record) Create() error {
	var jsonData = struct {
		FieldData map[string]interface{} `json:"fieldData"`
	}{
		r.StagedChanges,
	}

	//Create the request json body
	var requestBody, err = json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %v", err.Error())
	}

	//Build and send request to the host to create record
	req, err := http.NewRequest(
		"POST",
		r.Session.recordsURL(r.Layout, ""),
		bytes.NewBuffer(requestBody),
	)
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

	//Check for errors in the response
	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
	}

	//Update local record field data with staged changes
	for fieldName, value := range r.StagedChanges {
		r.FieldData[fieldName] = value
	}

	//Set the ID returned by the API
	r.ID = jsonRes.Response.RecordID

	//Build and send request to the host to get the default field data for the created record
	req, err = http.NewRequest(
		"GET",
		r.Session.recordsURL(r.Layout, r.ID),
		bytes.NewBuffer([]byte{}),
	)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+r.Session.Token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send GET request: %v", err.Error())
	}

	//Read the body
	resBodyBytes, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err.Error())
	}

	//Unmarshal json body
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return fmt.Errorf("failed to decode response body as json: %v", err.Error())
	}

	//Check for errors in the response
	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
	}

	//Parse the field data for the record
	for fieldname, val := range jsonRes.Response.Data[0].(map[string]interface{})["fieldData"].(map[string]interface{}) {
		r.FieldData[fieldname] = val
	}

	return nil
}

//Delete deletes the record using the same session the record was retrieved with
func (r *Record) Delete() error {
	//Build and send request to the host
	req, err := http.NewRequest(
		"DELETE",
		r.Session.recordsURL(r.Layout, r.ID),
		bytes.NewBuffer([]byte{}),
	)
	req.Header.Add("Authorization", "Bearer "+r.Session.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send DELETE request: %v", err.Error())
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

	//Check response code
	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
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
func (r *Record) String(fieldName string) string {
	data := r.Get(fieldName)

	switch data.(type) {
	case string:
		return data.(string)
	case nil:
		return ""
	}

	return ""
}

/*
Int gets the data in the specified field and returns it as an int.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Int(fieldName string) int {
	data := r.Get(fieldName)

	if val, ok := data.(float64); ok {
		return int(val)
	}

	return 0
}

/*
Int32 gets the data in the specified field and returns it as an int32.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Int32(fieldName string) int32 {
	data := r.Get(fieldName)

	if val, ok := data.(float64); ok {
		return int32(val)
	}

	return 0
}

/*
Int64 gets the data in the specified field and returns it as an int64.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Int64(fieldName string) int64 {
	data := r.Get(fieldName)

	if val, ok := data.(float64); ok {
		return int64(val)
	}

	return 0
}

/*
Float32 gets the data in the specified field and returns it as an float32.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Float32(fieldName string) float32 {
	data := r.Get(fieldName)

	if val, ok := data.(float64); ok {
		return float32(val)
	}

	return 0
}

/*
Float64 gets the data in the specified field and returns it as an float64.
The FileMaker database field needs to be a number field.
*/
func (r *Record) Float64(fieldName string) float64 {
	data := r.Get(fieldName)

	if val, ok := data.(float64); ok {
		return val
	}

	return 0
}

/*
Bool gets the data in the specified field and parses it as a bool, with empty
fields evaluating to `false` and non-empty text fields and number fields with
a value greater than 0 evaluating to `true`.
*/
func (r *Record) Bool(fieldName string) bool {
	data := r.Get(fieldName)

	switch data.(type) {
	case string:
		return len(data.(string)) > 0
	case float64:
		return data.(float64) > 0
	}

	return false
}

//Time gets the data in the specified field and attempts to parse it as a `time.Time` object.
func (r *Record) Time(fieldName string) time.Time {
	data := r.String(fieldName)

	if len(data) == 19 {
		//Attempt to parse as timestamp
		t, _ := time.ParseInLocation("01/02/2006 15:04:05", data, time.Local)
		return t
	} else if len(data) == 10 {
		//Attempt to parse as date
		t, _ := time.ParseInLocation("01/02/2006", data, time.Local)
		return t
	}

	return time.Time{}
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

- int8

- int16

- int32

- int64

- float32

- float64

- bool

- time.Time (date and timestamp fields)
*/
func (r *Record) Map(obj interface{}) {
	v := reflect.ValueOf(obj).Elem()

	//Loop through all struct fields
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)

		//Skip the field if it cannot be set
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		//Get the `fm` tag of the field
		tag := v.Type().Field(i).Tag.Get("fm")

		//Set the struct field value depending on the underlying type
		switch field.Interface().(type) {
		case string:
			field.SetString(r.String(tag))
		case int, int8, int16, int32, int64:
			field.SetInt(r.Int64(tag))
		case float32, float64:
			field.SetFloat(r.Float64(tag))
		case bool:
			field.SetBool(r.Bool(tag))
		case time.Time:
			field.Set(reflect.ValueOf(r.Time(tag)))
		}
	}
}
