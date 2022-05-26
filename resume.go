package filemaker

import "errors"

//Resume resumes a database session with the specified token
func Resume(host, database, username, password, token string) (*Session, error) {
	if host == "" {
		return nil, errors.New("no host specified")
	} else if database == "" {
		return nil, errors.New("no database specified")
	} else if username == "" {
		return nil, errors.New("no username specified")
	}

	//Determine protocol scheme
	var protocol = "https://"
	if len(host) >= 8 && host[0:8] == "https://" {
		protocol = ""
	}

	return &Session{
		Token:    token,
		Protocol: protocol,
		Host:     host,
		Database: database,
		Username: username,
		Password: password,
	}, nil
}
