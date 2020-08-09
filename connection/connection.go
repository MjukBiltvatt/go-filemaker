package connection

//Connection is a connection struct used for subsequent requests to the host
type Connection struct {
	Token    string
	Protocol string
	Host     string
	Database string
	Username string
	Password string
}
