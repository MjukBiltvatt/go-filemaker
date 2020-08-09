package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

//PerformFind performs the specified findrequest on the specified layout
func (conn *Connection) PerformFind(layout string, findRequest interface{}) ([]interface{}, error) {
	if layout == "" {
		return nil, errors.New("No layout specified")
	}

	type responseBody struct {
		Response struct {
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
		Messages []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"messages"`
	}

	//Create the request json body
	var requestBody, err = json.Marshal(findRequest)
	if err != nil {
		return nil, errors.New("Failed to marshal request body: " + err.Error())
	}

	//Build and send request to the host
	req, err := http.NewRequest("POST", conn.Protocol+conn.Host+"/fmi/data/v1/databases/"+conn.Database+"/layouts/"+layout+"/_find", bytes.NewBuffer(requestBody))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+conn.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("Failed to send POST request: " + err.Error())
	}

	fmt.Printf("\nPerforming find: " + res.Status + "\n")

	fmt.Println("requestBody: ", bytes.NewBuffer(requestBody))

	//Read the body
	resBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.New("Failed to read response body: " + err.Error())
	}
	fmt.Println("Response body:", string(resBodyBytes))

	//Unmarshal json body
	var jsonRes responseBody
	err = json.Unmarshal(resBodyBytes, &jsonRes)
	if err != nil {
		return nil, errors.New("Failed to decode response body as json: " + err.Error())
	}

	if jsonRes.Messages[0].Code != "0" {
		return nil, errors.New("Failed at host: " + jsonRes.Messages[0].Message)
	}

	return jsonRes.Response.Data, nil
}
