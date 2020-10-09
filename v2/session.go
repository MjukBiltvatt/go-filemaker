package filemaker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
)

//Session is used for subsequent requests to the host
type Session struct {
	Token    string
	Protocol string
	Host     string
	Database string
	Username string
	Password string
}

//ResponseBody represents the json body received from http requests to the filemaker api
type ResponseBody struct {
	Messages []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"messages"`
	Response struct {
		Token    string `json:"token"`
		ModID    string `json:"modId"`
		RecordID string `json:"recordId"`
		DataInfo struct {
			Database         string `json:"database"`
			Layout           string `json:"layout"`
			Table            string `json:"table"`
			TotalRecordCount int    `json:"totalRecordCount"`
			FoundCount       int    `json:"foundCount"`
			ReturnedCount    int    `json:"returnedCount"`
		} `json:"dataInfo"`
		Data []interface{} `json:"data"`
	} `json:"response"`
}

//Destroy logs out of the database session
func (s *Session) Destroy() error {
	//Build and send request to the host
	req, err := http.NewRequest("DELETE", s.Protocol+s.Host+"/fmi/data/v1/databases/"+s.Database+"/sessions/"+s.Token, bytes.NewBuffer([]byte{}))
	req.Header.Add("Content-Type", "application/json")
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

	return nil
}

//PerformFind performs the specified findcommand on the specified layout
func (s *Session) PerformFind(layout string, findCommand interface{}) ([]Record, error) {
	if layout == "" {
		return nil, errors.New("No layout specified")
	}

	//Create the request json body
	var requestBody, err = json.Marshal(findCommand)
	if err != nil {
		return nil, errors.New("Failed to marshal request body: " + err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest("POST", s.Protocol+s.Host+"/fmi/data/v1/databases/"+s.Database+"/layouts/"+layout+"/_find", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+s.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("Failed to send POST request: " + err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.New("Failed to read response body: " + err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return nil, errors.New("Failed to decode response body as json: " + err.Error())
	}

	//Check for errors
	if jsonRes.Messages[0].Code == "401" {
		//No records found, return empty slice
		return []Record{}, nil
	} else if jsonRes.Messages[0].Code != "0" {
		//Unknown error
		return nil, errors.New("Failed at host: " + jsonRes.Messages[0].Message + " (" + jsonRes.Messages[0].Code + ")")
	}

	var records []Record

	for _, r := range jsonRes.Response.Data {
		records = append(records, newRecord(layout, r, *s))
	}

	return records, nil
}

//CreateRecord returns a new empty record for the specified layout
func (s *Session) CreateRecord(layout string) Record {
	return Record{
		Layout:        layout,
		StagedChanges: make(map[string]interface{}),
		Session:       s,
	}
}
