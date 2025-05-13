package client

import (
	"errors"
	"net/http"

	"github.com/cloudflare/cloudflare-go/v4"
)

func IgnoreConflict(err error) error {
	return ignoreErr(err, IsConflict)
}

func IgnoreNotFound(err error) error {
	return ignoreErr(err, IsNotFound)
}

func IgnoreStatusCode(err error, code int) error {
	if IsStatusCode(err, code) {
		return nil
	} else {
		return err
	}
}

func IsConflict(err error) bool {
	return IsStatusCode(err, http.StatusConflict)
}

func IsNotFound(err error) bool {
	return IsStatusCode(err, http.StatusNotFound)
}

func IsStatusCode(err error, code int) bool {
	var apiError *cloudflare.Error
	if errors.As(err, &apiError) {
		return apiError.StatusCode == code
	} else {
		return false
	}
}

func ignoreErr(err error, pred func(error) bool) error {
	if pred(err) {
		return nil
	} else {
		return err
	}
}
