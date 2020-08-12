package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/jomla97/go-filemaker/pkg/record"
)

//Commit saves changes to the given record
func (sess *Session) Commit(r *record.Record) error {
	if r.ID != "" {
		return sess.CommitChanges(r)
	}

	return sess.CommitNew(r)
}

//CommitChanges saves changes to the given existing record
func (sess *Session) CommitChanges(r *record.Record) error {
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
	req, err := http.NewRequest("PATCH", sess.Protocol+sess.Host+"/fmi/data/v1/databases/"+sess.Database+"/layouts/"+r.Layout+"/records/"+r.ID, bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+sess.Token)
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

	return nil
}

//CommitNew uploads a new local record to the server
func (sess *Session) CommitNew(r *record.Record) error {
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
	req, err := http.NewRequest("POST", sess.Protocol+sess.Host+"/fmi/data/v1/databases/"+sess.Database+"/layouts/"+r.Layout+"/records", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+sess.Token)
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
