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

//New starts a database session
func New(host string, database string, username string, password string) (*Session, error) {
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
	var protocol = "https://"
	if len(host) >= 8 && host[0:8] == "https://" {
		protocol = ""
	}

	//Build and send request to the host
	req, err := http.NewRequest("POST", protocol+host+"/fmi/data/v1/databases/"+database+"/sessions", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
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

	if jsonRes.Messages[0].Code != "0" {
		return nil, fmt.Errorf("failed at host: %v (%v)", jsonRes.Messages[0].Message, jsonRes.Messages[0].Code)
	}

	return &Session{
		Token:    jsonRes.Response.Token,
		Protocol: protocol,
		Host:     host,
		Database: database,
		Username: username,
		Password: password,
	}, nil
}
