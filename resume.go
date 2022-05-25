package filemaker

import (
	"github.com/MjukBiltvatt/go-filemaker/pkg/session"
)

//Resume resumes a database session with the specified token
func Resume(host string, database string, username string, password string, token string) (*session.Session, error) {
	return session.Resume(host, database, username, password, token)
}
