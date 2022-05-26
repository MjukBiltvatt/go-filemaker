package filemaker

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

//Session is used for subsequent requests to the host
type Session struct {
	Token    string
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

//baseURL builds the base of the data API URL, containing protocol, host and database
func (s Session) baseURL() string {
	return fmt.Sprintf(
		"%s/fmi/data/v1/databases/%s",
		s.Host,
		s.Database,
	)
}

//recordsURL builds a data API URL used to access record(s)
func (s Session) recordsURL(layout, id string) string {
	base := fmt.Sprintf(
		"%s/layouts/%s/records",
		s.baseURL(),
		layout,
	)

	if id == "" {
		return base
	}

	return fmt.Sprintf("%s/%s", base, id)
}

//Destroy logs out of the database session
func (s *Session) Destroy() error {
	//Build and send request to the host
	req, err := http.NewRequest(
		"DELETE",
		fmt.Sprintf("%s/sessions/%s", s.baseURL(), s.Token),
		bytes.NewBuffer([]byte{}),
	)
	req.Header.Add("Content-Type", "application/json")
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

	if jsonRes.Messages[0].Code != "0" {
		return fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
	}

	return nil
}

//Find performs the specified findcommand on the specified layout
func (s *Session) Find(layout string, findCommand interface{}) ([]Record, error) {
	if layout == "" {
		return nil, errors.New("No layout specified")
	}

	//Create the request json body
	var requestBody, err = json.Marshal(findCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/layouts/%s/_find", s.baseURL(), layout),
		bytes.NewBuffer(requestBody),
	)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+s.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send POST request: %v", err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response body as json: %v", err.Error())
	}

	//Check for errors
	if jsonRes.Messages[0].Code == "401" {
		//No records found, return empty slice
		return []Record{}, nil
	} else if jsonRes.Messages[0].Code != "0" {
		//Unknown error
		return nil, fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
	}

	var records []Record

	for _, r := range jsonRes.Response.Data {
		records = append(records, newRecord(layout, r, *s))
	}

	return records, nil
}

//NewRecord returns a new empty record for the specified layout
func (s *Session) NewRecord(layout string) Record {
	return Record{
		Layout:        layout,
		FieldData:     make(map[string]interface{}),
		StagedChanges: make(map[string]interface{}),
		Session:       s,
	}
}

//New starts a database session
func New(host, database, username, password string) (*Session, error) {
	if host == "" {
		return nil, errors.New("No host specified")
	} else if database == "" {
		return nil, errors.New("No database specified")
	} else if username == "" {
		return nil, errors.New("No username specified")
	}

	//Create an empty json body
	var requestBody, err = json.Marshal(struct{}{})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err.Error())
	}

	//Determine protocol scheme
	if len(host) < 8 || host[:8] != "https://" {
		host = fmt.Sprintf("https://%s", host)
	}

	//Build and send request to the host
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/fmi/data/v1/databases/%s/sessions", host, database),
		bytes.NewBuffer(requestBody),
	)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add(
		"Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)),
	)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send POST request: %v", err.Error())
	}

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err.Error())
	}

	//Unmarshal json body
	var jsonRes ResponseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response body as json: %v", err.Error())
	}

	//Check the response code
	if jsonRes.Messages[0].Code != "0" {
		return nil, fmt.Errorf(
			"failed at host: %v (%v)",
			jsonRes.Messages[0].Message,
			jsonRes.Messages[0].Code,
		)
	}

	return &Session{
		Token:    jsonRes.Response.Token,
		Host:     host,
		Database: database,
		Username: username,
		Password: password,
	}, nil
}
