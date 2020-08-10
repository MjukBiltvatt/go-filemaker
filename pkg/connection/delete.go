package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

//Delete the given record
func (conn *Connection) Delete(layout string, id string) error {
	//Build and send request to the host
	req, err := http.NewRequest("DELETE", conn.Protocol+conn.Host+"/fmi/data/v1/databases/"+conn.Database+"/layouts/"+layout+"/records/"+id, bytes.NewBuffer([]byte{}))
	req.Header.Add("Authorization", "Bearer "+conn.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Failed to send DELETE request: " + err.Error())
	}

	fmt.Printf("\nDeleting record: " + res.Status + "\n")

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
		return errors.New("Failed at host: " + jsonRes.Messages[0].Message + " (" + jsonRes.Messages[0].Code + ")")
	}

	return nil
}
