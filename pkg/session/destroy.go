package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
)

//Destroy logs out of the database session
func (sess *Session) Destroy() error {
	//Build and send request to the host
	req, err := http.NewRequest("DELETE", sess.Protocol+sess.Host+"/fmi/data/v1/databases/"+sess.Database+"/sessions/"+sess.Token, bytes.NewBuffer([]byte{}))
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
