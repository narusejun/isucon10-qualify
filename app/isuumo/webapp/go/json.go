package main

import (
	"github.com/goccy/go-json"
	"github.com/labstack/echo"
)

// json json-iterator使用
func JSON(c echo.Context, code int, i interface{}) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	c.Response().WriteHeader(code)
	return json.NewEncoder(c.Response()).Encode(i)
}
