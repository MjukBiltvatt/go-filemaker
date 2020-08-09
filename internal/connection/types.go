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

//ResponseBody represents the json body received from http requests to the filemaker api
type ResponseBody struct {
	Messages []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"messages"`
	Response struct {
		Token    string `json:"token"`
		ModID    string `json:"modId"`
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
