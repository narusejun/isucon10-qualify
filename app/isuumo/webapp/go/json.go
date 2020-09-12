package main

import (
	jsoniter "github.com/json-iterator/go"
	"github.com/labstack/echo"
)

var json = jsoniter.Config{
	EscapeHTML:                    false,
	ObjectFieldMustBeSimpleString: true,
}.Froze()

// json json-iterator使用
func JSON(c echo.Context, code int, i interface{}) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	c.Response().WriteHeader(code)
	return json.NewEncoder(c.Response()).Encode(i)
}
