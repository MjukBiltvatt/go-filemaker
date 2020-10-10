package filemaker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
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
	r.StagedChanges[fieldName] = value
}

//GetField gets the value of a field in the given record
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
		return errors.New("Failed to marshal request body: " + err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest("PATCH", r.Session.Protocol+r.Session.Host+"/fmi/data/v1/databases/"+r.Session.Database+"/layouts/"+r.Layout+"/records/"+r.ID, bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+r.Session.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Failed to send PATCH request: " + err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return errors.New("Failed to read response body: " + err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return errors.New("Failed to decode response body as json: " + err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return errors.New("Failed at host: " + jsonRes.Messages[0].Message + " (" + jsonRes.Messages[0].Code + ")")
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
		return errors.New("Failed to marshal request body: " + err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest("POST", r.Session.Protocol+r.Session.Host+"/fmi/data/v1/databases/"+r.Session.Database+"/layouts/"+r.Layout+"/records", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+r.Session.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Failed to send POST request: " + err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return errors.New("Failed to read response body: " + err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return errors.New("Failed to decode response body as json: " + err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return errors.New("Failed at host: " + jsonRes.Messages[0].Message + " (" + jsonRes.Messages[0].Code + ")")
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
		return errors.New("Failed to send DELETE request: " + err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return errors.New("Failed to read response body: " + err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return errors.New("Failed to decode response body as json: " + err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return errors.New("Failed at host: " + jsonRes.Messages[0].Message + " (" + jsonRes.Messages[0].Code + ")")
	}

	//Empty the local record instance
	r.ID = ""
	r.StagedChanges = map[string]interface{}{}
	r.FieldData = map[string]interface{}{}

	return nil
}

//String gets the data in the specified field and returns it as a string
func (r *Record) String(fieldName string) (string, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(string); ok {
		return val, nil
	}

	return "", fmt.Errorf("field `%v` value is not of type string: %v", fieldName, data)
}

//Int gets the data in the specified field and returns it as an int
func (r *Record) Int(fieldName string) (int, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return int(val), nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type int: %v", fieldName, data)
}

//Int32 gets the data in the specified field and returns it as an int32
func (r *Record) Int32(fieldName string) (int32, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return int32(val), nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type int32: %v", fieldName, data)
}

//Int64 gets the data in the specified field and returns it as an int64
func (r *Record) Int64(fieldName string) (int64, error) {
	data := r.GetField(fieldName)

	if val, ok := data.(float64); ok {
		return int64(val), nil
	}

	return 0, fmt.Errorf("field `%v` value is not of type int64: %v", fieldName, data)
}
