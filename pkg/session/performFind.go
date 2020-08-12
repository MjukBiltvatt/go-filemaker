package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/jomla97/go-filemaker/pkg/errortypes"
	"github.com/jomla97/go-filemaker/pkg/record"
)

//PerformFind performs the specified findcommand on the specified layout
func (sess *Session) PerformFind(layout string, findCommand interface{}) ([]record.Record, error) {
	if layout == "" {
		return nil, errors.New("No layout specified")
	}

	//Create the request json body
	var requestBody, err = json.Marshal(findCommand)
	if err != nil {
		return nil, errors.New("Failed to marshal request body: " + err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest("POST", sess.Protocol+sess.Host+"/fmi/data/v1/databases/"+sess.Database+"/layouts/"+layout+"/_find", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+sess.Token)
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

	if jsonRes.Messages[0].Code == "401" {
		return nil, errortypes.NewNotFound()
	} else if jsonRes.Messages[0].Code != "0" {
		return nil, errors.New("Failed at host: " + jsonRes.Messages[0].Message + " (" + jsonRes.Messages[0].Code + ")")
	}

	var records []record.Record

	for _, r := range jsonRes.Response.Data {
		records = append(records, record.New(layout, r))
	}

	return records, nil
}
