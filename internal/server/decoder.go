package server

import "github.com/gorilla/schema"

// queryDecoder is a shared schema decoder used by all request handlers.
var queryDecoder = func() *schema.Decoder {
	d := schema.NewDecoder()
	d.SetAliasTag("query")
	return d
}()
