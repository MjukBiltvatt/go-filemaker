package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/jomla97/go-filemaker/internal/record"
)

//Commit saves changes to the given record
func (conn *Connection) Commit(r *record.Record) error {
	if r.ID != "" {
		return conn.CommitChanges(r)
	}

	return conn.CommitNew(r)
}

//CommitChanges saves changes to the given existing record
func (conn *Connection) CommitChanges(r *record.Record) error {
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
	req, err := http.NewRequest("PATCH", conn.Protocol+conn.Host+"/fmi/data/v1/databases/"+conn.Database+"/layouts/"+r.Layout+"/records/"+r.ID, bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+conn.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Failed to send PATCH request: " + err.Error())
	}

	fmt.Printf("\nCommitting record: " + res.Status + "\n")

	fmt.Println("requestBody: ", bytes.NewBuffer(requestBody))

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return errors.New("Failed to read response body: " + err.Error())
	}
	fmt.Println("Response body:", string(resBodyBytes))

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return errors.New("Failed to decode response body as json: " + err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return errors.New("Failed at host: " + jsonRes.Messages[0].Message)
	}

	return nil
}

//CommitNew uploads a new local record to the server
func (conn *Connection) CommitNew(r *record.Record) error {
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
	req, err := http.NewRequest("POST", conn.Protocol+conn.Host+"/fmi/data/v1/databases/"+conn.Database+"/layouts/"+r.Layout+"/records", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+conn.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Failed to send POST request: " + err.Error())
	}

	fmt.Printf("\nCommitting record: " + res.Status + "\n")

	fmt.Println("requestBody: ", bytes.NewBuffer(requestBody))

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return errors.New("Failed to read response body: " + err.Error())
	}
	fmt.Println("Response body:", string(resBodyBytes))

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return errors.New("Failed to decode response body as json: " + err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return errors.New("Failed at host: " + jsonRes.Messages[0].Message)
	}

	r.ID = jsonRes.Response.RecordID

	return nil
}
