package service

import "errors"

var (
	ErrUnauthorized   = errors.New("unauthorized")
	ErrNotFound       = errors.New("not found")
	ErrValidation     = errors.New("validation")
	ErrBinderfs       = errors.New("binderfs is unavailable on this host; a second instance was refused")
	ErrConflict       = errors.New("conflict")
	ErrLocked         = errors.New("device is controlled by another operator")
	ErrNotImplemented = errors.New("not implemented")
)
