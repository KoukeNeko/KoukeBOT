package llm

import "net/http"

type oauthHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

var oauthHTTPClient oauthHTTPDoer = http.DefaultClient
