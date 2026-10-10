package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
)

type structuredError struct {
	Name         string `json:"name"`
	Message      string `json:"message"`
	StatusCode   int    `json:"statusCode,omitempty"`
	URL          string `json:"url,omitempty"`
	ResponseBody string `json:"responseBody,omitempty"`
}

func structuredErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	se := structuredError{
		Name:    fmt.Sprintf("%T", err),
		Message: err.Error(),
	}

	v := reflect.ValueOf(err)
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		if f := v.FieldByName("StatusCode"); f.IsValid() && f.Kind() == reflect.Int {
			se.StatusCode = int(f.Int())
		}
		if f := v.FieldByName("Request"); f.IsValid() && f.Kind() == reflect.Pointer && !f.IsNil() {
			se.URL = requestURL(f)
		}
		if m := v.MethodByName("RawJSON"); m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() == 1 {
			if outs := m.Call(nil); len(outs) == 1 && outs[0].Kind() == reflect.String {
				se.ResponseBody = outs[0].String()
			}
		}
	}

	var urlErr *url.Error
	if se.URL == "" && errors.As(err, &urlErr) {
		se.URL = urlErr.URL
	}

	b, mErr := json.Marshal(se)
	if mErr != nil {
		return se.Message
	}
	return string(b)
}

func requestURL(reqVal reflect.Value) string {
	if reqVal.Kind() != reflect.Pointer || reqVal.IsNil() {
		return ""
	}
	req := reqVal.Elem()
	urlField := req.FieldByName("URL")
	if !urlField.IsValid() || urlField.Kind() != reflect.Pointer || urlField.IsNil() {
		return ""
	}
	if u, ok := urlField.Interface().(*url.URL); ok && u != nil {
		return u.String()
	}
	return ""
}
